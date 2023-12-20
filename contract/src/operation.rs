use coreum_wasm_sdk::{assetft, core::CoreumMsg};
use cosmwasm_schema::cw_serde;
use cosmwasm_std::{coin, Addr, Coin, CosmosMsg, Response, Storage, Uint128};

use crate::{
    contract::{convert_amount_decimals, XRPL_TOKENS_DECIMALS},
    error::ContractError,
    evidence::TransactionResult,
    signatures::Signature,
    state::{TokenState, COREUM_TOKENS, PENDING_OPERATIONS, REFUNDABLE_AMOUNTS, XRPL_TOKENS},
    token::build_xrpl_token_key,
};

#[cw_serde]
pub struct Operation {
    pub ticket_sequence: Option<u64>,
    pub account_sequence: Option<u64>,
    pub signatures: Vec<Signature>,
    pub operation_type: OperationType,
}

#[cw_serde]
pub enum OperationType {
    AllocateTickets {
        number: u32,
    },
    TrustSet {
        issuer: String,
        currency: String,
        trust_set_limit_amount: Uint128,
    },
    #[serde(rename = "coreum_to_xrpl_transfer")]
    CoreumToXRPLTransfer {
        issuer: String,
        currency: String,
        amount: Uint128,
        transfer_fee: Uint128,
        sender: Addr,
        recipient: String,
    },
}

pub fn check_operation_exists(
    storage: &mut dyn Storage,
    account_sequence: Option<u64>,
    ticket_sequence: Option<u64>,
) -> Result<u64, ContractError> {
    // Get the sequence or ticket number (priority for sequence number)
    let operation_id = account_sequence.unwrap_or_else(|| ticket_sequence.unwrap());

    if !PENDING_OPERATIONS.has(storage, operation_id) {
        return Err(ContractError::PendingOperationNotFound {});
    }

    Ok(operation_id)
}

pub fn create_pending_operation(
    storage: &mut dyn Storage,
    ticket_sequence: Option<u64>,
    account_sequence: Option<u64>,
    operation_type: OperationType,
) -> Result<(), ContractError> {
    let operation = Operation {
        ticket_sequence,
        account_sequence,
        signatures: vec![],
        operation_type,
    };

    let operation_id = ticket_sequence.unwrap_or_else(|| account_sequence.unwrap());
    if PENDING_OPERATIONS.has(storage, operation_id) {
        return Err(ContractError::PendingOperationAlreadyExists {});
    }
    PENDING_OPERATIONS.save(storage, operation_id, &operation)?;

    Ok(())
}

pub fn handle_trust_set_confirmation(
    storage: &mut dyn Storage,
    issuer: String,
    currency: String,
    transaction_result: TransactionResult,
) -> Result<(), ContractError> {
    let key = build_xrpl_token_key(issuer, currency);

    let mut token = XRPL_TOKENS
        .load(storage, key.clone())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    // Set token to active if TrustSet operation was successful
    if transaction_result.eq(&TransactionResult::Accepted) {
        token.state = TokenState::Enabled;
    } else {
        token.state = TokenState::Inactive;
    }

    XRPL_TOKENS.save(storage, key, &token)?;
    Ok(())
}

pub fn handle_coreum_to_xrpl_transfer_confirmation(
    storage: &mut dyn Storage,
    transaction_result: TransactionResult,
    operation_id: u64,
    response: &mut Response<CoreumMsg>,
) -> Result<(), ContractError> {
    let pending_operation = PENDING_OPERATIONS
        .load(storage, operation_id)
        .map_err(|_| ContractError::PendingOperationNotFound {})?;

    match pending_operation.operation_type {
        OperationType::CoreumToXRPLTransfer {
            issuer,
            currency,
            amount,
            transfer_fee,
            sender,
            ..
        } => {
            // We check that the token that was sent was an XRPL originated token:
            let key = build_xrpl_token_key(issuer, currency.to_owned());
            match XRPL_TOKENS.may_load(storage, key)? {
                Some(xrpl_token) => {
                    // If transaction was accepted and the token that was sent back was an XRPL originated token, we must burn the token amount and transfer_fee
                    if transaction_result.eq(&TransactionResult::Accepted) {
                        let burn_msg = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Burn {
                            coin: coin(
                                amount.checked_add(transfer_fee)?.u128(),
                                xrpl_token.coreum_denom,
                            ),
                        }));

                        *response = response.to_owned().add_message(burn_msg);
                    } else {
                        // If transaction was rejected, we must store the amount and transfer_fee so that sender can claim it back.
                        store_refundable_amount(
                            storage,
                            sender,
                            coin(
                                amount.checked_add(transfer_fee)?.u128(),
                                xrpl_token.coreum_denom,
                            ),
                        )?;
                    }
                }
                None => {
                    // If the token sent was a Coreum originated token we only need to store refundable amount in case of rejection.
                    if transaction_result.ne(&TransactionResult::Accepted) {
                        match COREUM_TOKENS
                            .idx
                            .xrpl_currency
                            .item(storage, currency)?
                            .map(|(_, ct)| ct)
                        {
                            Some(token) => {
                                // We need to convert the decimals to coreum decimals
                                let amount_to_send_back = convert_amount_decimals(
                                    XRPL_TOKENS_DECIMALS,
                                    token.decimals,
                                    amount,
                                )?;
                                // If transaction was rejected, we must store the amount and transfer_fee so that sender can claim it back.
                                store_refundable_amount(
                                    storage,
                                    sender,
                                    coin(amount_to_send_back.u128(), token.denom),
                                )?;
                            }
                            // In practice this will never happen because any token issued from the multisig address is a token that was bridged from Coreum so it will be registered.
                            // This could theoretically happen if the multisig address on XRPL issued a token on its own and then tried to bridge it
                            None => return Err(ContractError::TokenNotRegistered {}),
                        };
                    }
                }
            }
        }

        // We will never get into this case unless relayers misbehave (send an CoreumToXRPLTransfer operation result for a different operation type)
        _ => return Err(ContractError::InvalidOperationResult {}),
    }

    Ok(())
}

pub fn store_refundable_amount(
    storage: &mut dyn Storage,
    receiver: Addr,
    coin: Coin,
) -> Result<(), ContractError> {
    // We get current refundable amounts for the receiver.  If it's the first time we are storing an amount for this receiver, we initialize the array.
    let mut refundable_amounts = match REFUNDABLE_AMOUNTS.may_load(storage, receiver.to_owned())? {
        Some(refundable_amounts) => refundable_amounts,
        None => vec![],
    };
    // If we already have the coin in the fee collected array, we update the amount, if not, we add it as a new element.
    match refundable_amounts
        .iter_mut()
        .find(|c| c.denom == coin.denom)
    {
        Some(found_coin) => found_coin.amount += coin.amount,
        None => refundable_amounts.push(coin),
    }

    REFUNDABLE_AMOUNTS.save(storage, receiver, &refundable_amounts)?;

    Ok(())
}

pub fn check_and_update_refundable_amounts(
    storage: &mut dyn Storage,
    sender: Addr,
    amounts: &Vec<Coin>,
) -> Result<(), ContractError> {
    let mut refundable_amounts = REFUNDABLE_AMOUNTS.load(storage, sender.to_owned())?;
    // We are going to check if the amounts sent to claim are available in the refundable amounts and if they are, substract the amount from the refundable amounts.
    // If they are not, we are going to cancel the claiming operation.
    for coin in amounts {
        match refundable_amounts
            .iter_mut()
            .find(|f| f.denom == coin.denom && f.amount >= coin.amount)
        {
            Some(found_coin) => found_coin.amount -= coin.amount,
            None => {
                return Err(ContractError::AmountNotRefundable {
                    denom: coin.denom.to_owned(),
                    amount: coin.amount,
                })
            }
        }
    }

    // Clean if amount is zero
    refundable_amounts.retain(|c| !c.amount.is_zero());

    REFUNDABLE_AMOUNTS.save(storage, sender, &refundable_amounts)?;

    Ok(())
}
