use coreum_wasm_sdk::{assetft, core::CoreumMsg};
use cosmwasm_schema::cw_serde;
use cosmwasm_std::{coin, Addr, Coin, CosmosMsg, Response, Storage, Uint128};

use crate::{
    contract::{convert_amount_decimals, XRPL_TOKENS_DECIMALS},
    error::ContractError,
    evidence::TransactionResult,
    relayer::Relayer,
    signatures::Signature,
    state::{
        BridgeState, PendingRefund, TokenState, CONFIG, COREUM_TOKENS, PENDING_OPERATIONS,
        PENDING_REFUNDS, PENDING_ROTATE_KEYS, XRPL_TOKENS,
    },
    token::build_xrpl_token_key,
};

#[cw_serde]
pub struct Operation {
    pub id: String,
    // version will be used to handle changes in xrpl_base_fee.
    // If xrpl_base_fee changes, the version of operation will be increased by 1 (it's always created with an initial version = 1)
    // This way, relayers can know if they need to provide the signature again, for this version
    pub version: u64,
    pub ticket_sequence: Option<u64>,
    pub account_sequence: Option<u64>,
    pub signatures: Vec<Signature>,
    pub operation_type: OperationType,
    // xrpl_base_fee must be part of operation too to avoid race conditions
    pub xrpl_base_fee: u64,
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
    RotateKeys {
        new_relayers: Vec<Relayer>,
        new_evidence_threshold: u32,
    },
    #[serde(rename = "coreum_to_xrpl_transfer")]
    CoreumToXRPLTransfer {
        issuer: String,
        currency: String,
        amount: Uint128,
        max_amount: Option<Uint128>,
        sender: Addr,
        recipient: String,
    },
}

// For responses
impl OperationType {
    pub fn as_str(&self) -> &'static str {
        match self {
            OperationType::AllocateTickets { .. } => "allocate_tickets",
            OperationType::TrustSet { .. } => "trust_set",
            OperationType::RotateKeys { .. } => "rotate_keys",
            OperationType::CoreumToXRPLTransfer { .. } => "coreum_to_xrpl_transfer",
        }
    }
}

pub fn check_operation_exists(
    storage: &mut dyn Storage,
    operation_id: u64,
) -> Result<Operation, ContractError> {
    let operation = PENDING_OPERATIONS
        .load(storage, operation_id)
        .map_err(|_| ContractError::PendingOperationNotFound {})?;

    Ok(operation)
}

pub fn create_pending_operation(
    storage: &mut dyn Storage,
    timestamp: u64,
    ticket_sequence: Option<u64>,
    account_sequence: Option<u64>,
    operation_type: OperationType,
) -> Result<(), ContractError> {
    let config = CONFIG.load(storage)?;

    // If bridge is halted we prohibit all operation creations except allowed ones
    if config.bridge_state.eq(&BridgeState::Halted) {
        check_valid_operation_during_halt(storage, &operation_type)?
    }

    let operation_id = ticket_sequence.unwrap_or_else(|| account_sequence.unwrap());
    // We use a unique ID for operations that will also be used for refunding failed operations
    // We need to use both timestamp and operation_id to ensure uniqueness of IDs, since operation_id can be reused in case of invalid transactions
    let operation = Operation {
        id: format!("{}-{}", timestamp, operation_id),
        // Operations are initially created with version 1
        version: 1,
        ticket_sequence,
        account_sequence,
        signatures: vec![],
        operation_type,
        xrpl_base_fee: config.xrpl_base_fee,
    };

    if PENDING_OPERATIONS.has(storage, operation_id) {
        return Err(ContractError::PendingOperationAlreadyExists {});
    }
    PENDING_OPERATIONS.save(storage, operation_id, &operation)?;

    Ok(())
}

pub fn handle_trust_set_confirmation(
    storage: &mut dyn Storage,
    issuer: &String,
    currency: &String,
    transaction_result: &TransactionResult,
) -> Result<(), ContractError> {
    let key = build_xrpl_token_key(issuer.to_owned(), currency.to_owned());

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
            max_amount,
            sender,
            ..
        } => {
            // We check that the token that was sent was an XRPL originated token:
            let key = build_xrpl_token_key(issuer, currency.to_owned());
            match XRPL_TOKENS.may_load(storage, key)? {
                Some(xrpl_token) => {
                    // if operation was with XRP, max amount might be empty so we will use amount.
                    let amount_sent = max_amount.unwrap_or(amount);
                    // If transaction was accepted and the token that was sent back was an XRPL originated token, we must burn the token amount
                    if transaction_result.eq(&TransactionResult::Accepted) {
                        let burn_msg = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Burn {
                            coin: coin(amount_sent.u128(), xrpl_token.coreum_denom),
                        }));

                        *response = response.to_owned().add_message(burn_msg);
                    } else {
                        // If transaction was rejected, we must store the amount so that sender can claim it back
                        store_pending_refund(
                            storage,
                            pending_operation.id,
                            sender,
                            coin(amount_sent.u128(), xrpl_token.coreum_denom),
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
                                    max_amount.unwrap(),
                                )?;
                                // If transaction was rejected, we must store the amount so that sender can claim it back.
                                store_pending_refund(
                                    storage,
                                    pending_operation.id,
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

pub fn store_pending_refund(
    storage: &mut dyn Storage,
    pending_operation_id: String,
    receiver: Addr,
    coin: Coin,
) -> Result<(), ContractError> {
    // We store the pending refund for this user and this pending_operation_id
    let pending_refund = PendingRefund {
        address: receiver.to_owned(),
        id: pending_operation_id.to_owned(),
        coin,
    };

    PENDING_REFUNDS.save(storage, (receiver, pending_operation_id), &pending_refund)?;

    Ok(())
}

pub fn remove_pending_refund(
    storage: &mut dyn Storage,
    sender: Addr,
    pending_refund_id: String,
) -> Result<Coin, ContractError> {
    // If pending refund is not found we return the error
    let pending_refund = PENDING_REFUNDS
        .load(storage, (sender.to_owned(), pending_refund_id.to_owned()))
        .map_err(|_| ContractError::PendingRefundNotFound {})?;

    PENDING_REFUNDS.remove(storage, (sender, pending_refund_id))?;

    Ok(pending_refund.coin)
}

pub fn check_valid_operation_during_halt(
    storage: &mut dyn Storage,
    operation_type: &OperationType,
) -> Result<(), ContractError> {
    match &operation_type {
        // Only RotateKeys operations (if there is a pending rotate keys ongoing) or ticket allocations are allowed during bridge halt
        OperationType::RotateKeys { .. } => {
            if !PENDING_ROTATE_KEYS.load(storage)? {
                return Err(ContractError::BridgeHalted {});
            }
        }
        OperationType::AllocateTickets { .. } => (),
        _ => return Err(ContractError::BridgeHalted {}),
    }

    Ok(())
}
