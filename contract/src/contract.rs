use std::collections::VecDeque;

use crate::{
    error::ContractError,
    evidence::{handle_evidence, hash_bytes, Evidence, OperationResult, TransactionResult},
    msg::{
        AvailableTicketsResponse, CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg,
        InstantiateMsg, PendingOperationsResponse, QueryMsg, XRPLTokensResponse,
    },
    operation::{
        check_operation_exists, create_pending_operation, handle_trust_set_confirmation, Operation,
        OperationType,
    },
    relayer::{assert_relayer, validate_relayers, validate_xrpl_address},
    signatures::add_signature,
    state::{
        Config, ContractActions, CoreumToken, TokenState, XRPLToken, AVAILABLE_TICKETS, CONFIG,
        COREUM_TOKENS, PENDING_OPERATIONS, PENDING_TICKET_UPDATE, USED_TICKETS_COUNTER,
        XRPL_TOKENS,
    },
    tickets::{allocate_ticket, handle_ticket_allocation_confirmation, register_used_ticket},
};

use coreum_wasm_sdk::{
    assetft::{self, Msg::Issue, ParamsResponse, Query, BURNING, IBC, MINTING},
    core::{CoreumMsg, CoreumQueries, CoreumResult},
};
use cosmwasm_std::{
    coin, coins, entry_point, to_json_binary, Addr, BankMsg, Binary, CosmosMsg, Deps, DepsMut, Env,
    MessageInfo, Order, Response, StdResult, Uint128,
};
use cw2::set_contract_version;
use cw_ownable::{assert_owner, get_ownership, initialize_owner, Action};
use cw_utils::one_coin;

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

const MAX_PAGE_LIMIT: u32 = 250;
const MIN_SENDING_PRECISION: i32 = -15;
const MAX_SENDING_PRECISION: i32 = 15;

const XRP_SYMBOL: &str = "XRP";
const XRP_SUBUNIT: &str = "drop";
const XRP_DECIMALS: u32 = 6;

const COREUM_CURRENCY_PREFIX: &str = "coreum";
const XRPL_DENOM_PREFIX: &str = "xrpl";
const XRPL_TOKENS_DECIMALS: u32 = 15;

const XRP_CURRENCY: &str = "XRP";
const XRP_ISSUER: &str = "rrrrrrrrrrrrrrrrrrrrrho";

// Initial values for the XRP token that can be modified afterwards.
const XRP_DEFAULT_SENDING_PRECISION: i32 = 6;
const XRP_DEFAULT_MAX_HOLDING_AMOUNT: u128 =
    10u128.pow(16 - XRP_DEFAULT_SENDING_PRECISION as u32 + XRP_DECIMALS);

pub const MAX_TICKETS: u32 = 250;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut<CoreumQueries>,
    env: Env,
    info: MessageInfo,
    msg: InstantiateMsg,
) -> CoreumResult<ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    initialize_owner(
        deps.storage,
        deps.api,
        Some(deps.api.addr_validate(msg.owner.as_ref())?.as_ref()),
    )?;

    validate_relayers(&deps, msg.relayers.clone())?;

    validate_xrpl_address(msg.bridge_xrpl_address.to_owned())?;

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;

    // Threshold can't be more than number of relayers
    if msg.evidence_threshold > msg.relayers.len().try_into().unwrap() {
        return Err(ContractError::InvalidThreshold {});
    }

    // We need to allow at least 2 tickets and less than 250 (XRPL limit) to be used
    if msg.used_ticket_sequence_threshold <= 1 || msg.used_ticket_sequence_threshold > MAX_TICKETS {
        return Err(ContractError::InvalidUsedTicketSequenceThreshold {});
    }

    // We initialize these values here so that we can immediately start working with them
    USED_TICKETS_COUNTER.save(deps.storage, &0)?;
    PENDING_TICKET_UPDATE.save(deps.storage, &false)?;
    AVAILABLE_TICKETS.save(deps.storage, &VecDeque::new())?;

    let config = Config {
        relayers: msg.relayers,
        evidence_threshold: msg.evidence_threshold,
        used_ticket_sequence_threshold: msg.used_ticket_sequence_threshold,
        trust_set_limit_amount: msg.trust_set_limit_amount,
        bridge_xrpl_address: msg.bridge_xrpl_address,
    };

    CONFIG.save(deps.storage, &config)?;

    let xrp_issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: XRP_SYMBOL.to_string(),
        subunit: XRP_SUBUNIT.to_string(),
        precision: XRP_DECIMALS,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, BURNING, IBC]),
        burn_rate: "0.0".to_string(),
        send_commission_rate: "0.0".to_string(),
        uri: None,
        uri_hash: None,
    }));

    let xrp_coreum_denom = format!("{}-{}", XRP_SUBUNIT, env.contract.address).to_lowercase();

    // We save the link between the denom in the coreum chain and the denom in XRPL, so that when we receive
    // a token we can inform the relayers of what is being sent back.
    let token = XRPLToken {
        issuer: XRP_ISSUER.to_string(),
        currency: XRP_CURRENCY.to_string(),
        coreum_denom: xrp_coreum_denom,
        sending_precision: XRP_DEFAULT_SENDING_PRECISION,
        max_holding_amount: Uint128::new(XRP_DEFAULT_MAX_HOLDING_AMOUNT),
        // The XRP token is enabled from the start because it doesn't need approval to be received on the XRPL side.
        state: TokenState::Enabled,
    };

    let key = build_xrpl_token_key(XRP_ISSUER.to_string(), XRP_CURRENCY.to_string());
    XRPL_TOKENS.save(deps.storage, key, &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::Instantiation.as_str())
        .add_attribute("contract_name", CONTRACT_NAME)
        .add_attribute("contract_version", CONTRACT_VERSION)
        .add_attribute("owner", info.sender)
        .add_message(xrp_issue_msg))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut<CoreumQueries>,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> CoreumResult<ContractError> {
    match msg {
        ExecuteMsg::UpdateOwnership(action) => {
            update_ownership(deps.into_empty(), env, info, action)
        }
        ExecuteMsg::RegisterCoreumToken {
            denom,
            decimals,
            sending_precision,
            max_holding_amount,
        } => register_coreum_token(
            deps.into_empty(),
            env,
            info.sender,
            denom,
            decimals,
            sending_precision,
            max_holding_amount,
        ),
        ExecuteMsg::RegisterXRPLToken {
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
        } => register_xrpl_token(
            deps,
            env,
            info,
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
        ),
        ExecuteMsg::SaveEvidence { evidence } => {
            save_evidence(deps.into_empty(), info.sender, evidence)
        }
        ExecuteMsg::RecoverTickets {
            account_sequence,
            number_of_tickets,
        } => recover_tickets(
            deps.into_empty(),
            info.sender,
            account_sequence,
            number_of_tickets,
        ),
        ExecuteMsg::RecoverXRPLTokenRegistration { issuer, currency } => {
            recover_xrpl_token_registration(deps.into_empty(), info.sender, issuer, currency)
        }
        ExecuteMsg::SaveSignature {
            operation_id,
            signature,
        } => save_signature(deps.into_empty(), info.sender, operation_id, signature),
        ExecuteMsg::SendToXRPL { recipient } => {
            send_to_xrpl(deps.into_empty(), env, info, recipient)
        }
    }
}

fn update_ownership(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    action: Action,
) -> CoreumResult<ContractError> {
    let ownership = cw_ownable::update_ownership(deps, &env.block, &info.sender, action)?;
    Ok(Response::new().add_attributes(ownership.into_attributes()))
}

fn register_coreum_token(
    deps: DepsMut,
    env: Env,
    sender: Addr,
    denom: String,
    decimals: u32,
    sending_precision: i32,
    max_holding_amount: Uint128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &sender)?;

    // Minimum and maximum sending precisions we allow
    if !(MIN_SENDING_PRECISION..=MAX_SENDING_PRECISION).contains(&sending_precision) {
        return Err(ContractError::InvalidSendingPrecision {});
    }

    if sending_precision > decimals.try_into().unwrap() {
        return Err(ContractError::TokenSendingPrecisionTooHigh {});
    }

    if COREUM_TOKENS.has(deps.storage, denom.clone()) {
        return Err(ContractError::CoreumTokenAlreadyRegistered { denom });
    }

    // We generate a currency creating a Sha256 hash of the denom, the decimals and the current time so that if it fails we can try again
    let to_hash = format!("{}{}{}", denom, decimals, env.block.time.seconds()).into_bytes();
    let hex_string = hash_bytes(to_hash)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    // Format will be the hex representation in XRPL of the string coreum<hash>
    let xrpl_currency =
        convert_currency_to_xrpl_hexadecimal(format!("{}{}", COREUM_CURRENCY_PREFIX, hex_string));

    // We check that the this currency is not used already (we got the same hash)
    if COREUM_TOKENS
        .idx
        .xrpl_currency
        .item(deps.storage, xrpl_currency.to_owned())?
        .is_some()
    {
        return Err(ContractError::RegistrationFailure {});
    }

    let token = CoreumToken {
        denom: denom.clone(),
        decimals,
        xrpl_currency: xrpl_currency.to_owned(),
        sending_precision,
        max_holding_amount,
        // All registered Coreum originated tokens will start as enabled because they don't need a TrustSet operation to be bridged because issuer for such tokens is bridge address
        state: TokenState::Enabled,
    };
    COREUM_TOKENS.save(deps.storage, denom.clone(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RegisterCoreumToken.as_str())
        .add_attribute("denom", denom)
        .add_attribute("decimals", decimals.to_string())
        .add_attribute("xrpl_currency_for_denom", xrpl_currency))
}

fn register_xrpl_token(
    deps: DepsMut<CoreumQueries>,
    env: Env,
    info: MessageInfo,
    issuer: String,
    currency: String,
    sending_precision: i32,
    max_holding_amount: Uint128,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;

    validate_xrpl_issuer_and_currency(issuer.clone(), currency.clone())?;

    // Minimum and maximum sending precisions we allow
    if !(MIN_SENDING_PRECISION..=MAX_SENDING_PRECISION).contains(&sending_precision) {
        return Err(ContractError::InvalidSendingPrecision {});
    }

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;
    let key = build_xrpl_token_key(issuer.clone(), currency.clone());

    if XRPL_TOKENS.has(deps.storage, key.clone()) {
        return Err(ContractError::XRPLTokenAlreadyRegistered { issuer, currency });
    }

    // We generate a denom creating a Sha256 hash of the issuer, currency and current time
    let to_hash = format!("{}{}{}", issuer, currency, env.block.time.seconds()).into_bytes();

    // We encode the hash in hexadecimal and take the first 10 characters
    let hex_string = hash_bytes(to_hash)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    // Symbol and subunit we will use for the issued token in Coreum
    let symbol_and_subunit = format!("{}{}", XRPL_DENOM_PREFIX, hex_string);

    let issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: symbol_and_subunit.to_uppercase(),
        subunit: symbol_and_subunit.clone(),
        precision: XRPL_TOKENS_DECIMALS,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, BURNING, IBC]),
        burn_rate: "0.0".to_string(),
        send_commission_rate: "0.0".to_string(),
        uri: None,
        uri_hash: None,
    }));

    // Denom that token will have in Coreum
    let denom = format!("{}-{}", symbol_and_subunit, env.contract.address).to_lowercase();

    // This in theory is not necessary because issue_msg would fail if the denom already exists but it's a double check and a way to return a more readable error.
    if COREUM_TOKENS.has(deps.storage, denom.clone()) {
        return Err(ContractError::RegistrationFailure {});
    };

    let token = XRPLToken {
        issuer: issuer.clone(),
        currency: currency.clone(),
        coreum_denom: denom.clone(),
        sending_precision,
        max_holding_amount,
        // Registered tokens will start in processing until TrustSet operation is accepted/rejected
        state: TokenState::Processing,
    };

    XRPL_TOKENS.save(deps.storage, key, &token)?;

    // Create the pending operation to approve the token
    let config = CONFIG.load(deps.storage)?;
    let ticket = allocate_ticket(deps.storage)?;

    create_pending_operation(
        deps.storage,
        Some(ticket),
        None,
        OperationType::TrustSet {
            issuer: issuer.clone(),
            currency: currency.clone(),
            trust_set_limit_amount: config.trust_set_limit_amount,
        },
    )?;

    Ok(Response::new()
        .add_message(issue_msg)
        .add_attribute("action", ContractActions::RegisterXRPLToken.as_str())
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency)
        .add_attribute("denom", denom))
}

fn save_evidence(deps: DepsMut, sender: Addr, evidence: Evidence) -> CoreumResult<ContractError> {
    evidence.validate_basic()?;

    assert_relayer(deps.as_ref(), sender.clone())?;

    let threshold_reached = handle_evidence(deps.storage, sender, evidence.clone())?;

    let mut response = Response::new();

    match evidence {
        Evidence::XRPLToCoreumTransfer {
            tx_hash,
            issuer,
            currency,
            amount,
            recipient,
        } => {
            deps.api.addr_validate(recipient.as_ref())?;
            let config = CONFIG.load(deps.storage)?;

            // This means the token is not a Coreum originated token (the issuer is not the XRPL multisig address)
            if issuer.ne(&config.bridge_xrpl_address) {
                // Create issuer+currency key to find denom on coreum.
                let key = build_xrpl_token_key(issuer.clone(), currency.clone());

                let token = XRPL_TOKENS
                    .load(deps.storage, key)
                    .map_err(|_| ContractError::TokenNotRegistered {})?;

                if token.state.ne(&TokenState::Enabled) {
                    return Err(ContractError::XRPLTokenNotEnabled {});
                }

                let decimals = match is_token_xrp(token.issuer, token.currency) {
                    true => XRP_DECIMALS,
                    false => XRPL_TOKENS_DECIMALS,
                };

                // Here we simply truncate because the Coreum tokens corresponding to XRPL originated tokens have the same decimals as their corresponding Coreum tokens
                let amount_to_send = truncate_amount(token.sending_precision, decimals, amount)?;

                if amount_to_send
                    .checked_add(
                        deps.querier
                            .query_supply(token.coreum_denom.clone())?
                            .amount,
                    )?
                    .gt(&token.max_holding_amount)
                {
                    return Err(ContractError::MaximumBridgedAmountReached {});
                }

                if threshold_reached {
                    let mint_msg = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Mint {
                        coin: coin(amount_to_send.u128(), token.coreum_denom),
                        recipient: Some(recipient.to_string()),
                    }));

                    response = response.add_message(mint_msg)
                }
            } else {
                // We check that the token is registered and enabled
                let token = match COREUM_TOKENS
                    .idx
                    .xrpl_currency
                    .item(deps.storage, currency.to_owned())?
                    .map(|(_, ct)| ct)
                {
                    Some(token) => {
                        if token.state.ne(&TokenState::Enabled) {
                            return Err(ContractError::CoreumOriginatedTokenDisabled {});
                        }
                        token
                    }
                    // In practice this will never happen because any token issued from the multisig address is a token that was bridged from Coreum so it will be registered.
                    // This could theoretically happen if the multisig address on XRPL issued a token on its own and then tried to bridge it
                    None => return Err(ContractError::TokenNotRegistered {}),
                };

                // We first convert the amount we receive with XRPL decimals to the corresponding decimals in Coreum and then we apply the truncation according to sending precision.
                let amount_to_send = convert_and_truncate_amount(
                    token.sending_precision,
                    XRPL_TOKENS_DECIMALS,
                    token.decimals,
                    amount,
                )?;

                if threshold_reached {
                    // TODO(keyleu): for now we are SENDING back the entire amount but when fees are implemented this will not happen and part of the amount will be sent and funds will be collected
                    let send_msg = BankMsg::Send {
                        to_address: recipient.to_string(),
                        amount: coins(amount_to_send.u128(), token.denom),
                    };
                    response = response.add_message(send_msg);
                }
            }

            response = response
                .add_attribute("action", ContractActions::SendFromXRPLToCoreum.as_str())
                .add_attribute("hash", tx_hash)
                .add_attribute("issuer", issuer)
                .add_attribute("currency", currency)
                .add_attribute("amount", amount.to_string())
                .add_attribute("recipient", recipient.to_string())
                .add_attribute("threshold_reached", threshold_reached.to_string())
        }
        Evidence::XRPLTransactionResult {
            tx_hash,
            account_sequence,
            ticket_sequence,
            transaction_result,
            operation_result,
        } => {
            let operation_id =
                check_operation_exists(deps.storage, account_sequence, ticket_sequence)?;

            // custom state validation of the transaction results for operations
            // TODO(keyleu) clean up at end of development unifying operations that we don't need to check
            match &operation_result {
                OperationResult::TrustSet { issuer, currency } => {
                    let key = build_xrpl_token_key(issuer.to_owned(), currency.to_owned());

                    // We validate that the token is indeed registered and is in the processing state
                    let token = XRPL_TOKENS
                        .load(deps.storage, key)
                        .map_err(|_| ContractError::TokenNotRegistered {})?;

                    if token.state.ne(&TokenState::Processing) {
                        return Err(ContractError::XRPLTokenNotInProcessing {});
                    }
                }
                OperationResult::TicketsAllocation { .. } => {}
                OperationResult::CoreumToXRPLTransfer { .. } => {}
            }

            if threshold_reached {
                match &operation_result {
                    OperationResult::TicketsAllocation { tickets } => {
                        handle_ticket_allocation_confirmation(
                            deps.storage,
                            tickets.clone(),
                            transaction_result.clone(),
                        )?;
                    }
                    OperationResult::TrustSet { issuer, currency } => {
                        handle_trust_set_confirmation(
                            deps.storage,
                            issuer.to_owned(),
                            currency.to_owned(),
                            transaction_result.clone(),
                        )?;
                    }
                    OperationResult::CoreumToXRPLTransfer {} => (),
                }
                PENDING_OPERATIONS.remove(deps.storage, operation_id);

                if transaction_result.ne(&TransactionResult::Invalid) && ticket_sequence.is_some() {
                    // If the operation must trigger a new ticket allocation we must know if we can trigger it
                    // or not (if we have tickets available). Therefore we will return a false flag if
                    // we don't have available tickets left and we will notify with an attribute.
                    // NOTE: This will only happen in the particular case of a rejected ticket allocation
                    // operation.
                    if !register_used_ticket(deps.storage)? {
                        response = response.add_attribute(
                            "adding_ticket_allocation_operation_success",
                            false.to_string(),
                        );
                    }
                };
            }

            response = response
                .add_attribute("action", ContractActions::XRPLTransactionResult.as_str())
                .add_attribute("operation_result", operation_result.as_str())
                .add_attribute("operation_id", operation_id.to_string())
                .add_attribute("transaction_result", transaction_result.as_str())
                .add_attribute("threshold_reached", threshold_reached.to_string());

            if let Some(tx_hash) = tx_hash {
                response = response.add_attribute("tx_hash", tx_hash)
            }
        }
    }

    Ok(response)
}

fn recover_tickets(
    deps: DepsMut,
    sender: Addr,
    account_sequence: u64,
    number_of_tickets: Option<u32>,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &sender)?;

    let available_tickets = AVAILABLE_TICKETS.load(deps.storage)?;

    if !available_tickets.is_empty() {
        return Err(ContractError::StillHaveAvailableTickets {});
    }

    let pending_ticket_update = PENDING_TICKET_UPDATE.load(deps.storage)?;

    if pending_ticket_update {
        return Err(ContractError::PendingTicketUpdate {});
    }

    let used_tickets = USED_TICKETS_COUNTER.load(deps.storage)?;

    PENDING_TICKET_UPDATE.save(deps.storage, &true)?;
    // If we don't provide a number of tickets to recover we will recover the ones that we already used.
    let number_to_allocate = number_of_tickets.unwrap_or(used_tickets);

    let config = CONFIG.load(deps.storage)?;
    // We check that number_to_allocate > config.used_ticket_sequence_threshold in order to cover the
    // reallocation with just one XRPL transaction, otherwise the relocation might cause the
    // additional reallocation.
    if number_to_allocate <= config.used_ticket_sequence_threshold
        || number_to_allocate > MAX_TICKETS
    {
        return Err(ContractError::InvalidTicketSequenceToAllocate {});
    }

    create_pending_operation(
        deps.storage,
        None,
        Some(account_sequence),
        OperationType::AllocateTickets {
            number: number_to_allocate,
        },
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RecoverTickets.as_str())
        .add_attribute("account_sequence", account_sequence.to_string()))
}

fn recover_xrpl_token_registration(
    deps: DepsMut,
    sender: Addr,
    issuer: String,
    currency: String,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &sender)?;

    let key = build_xrpl_token_key(issuer.to_owned(), currency.to_owned());

    let mut token = XRPL_TOKENS
        .load(deps.storage, key.to_owned())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    // Check that the token is in inactive state, which means the trust set operation failed.
    if token.state.ne(&TokenState::Inactive) {
        return Err(ContractError::XRPLTokenNotInactive {});
    }

    // Put the state back to Processing since we are going to try to activate it again.
    token.state = TokenState::Processing;
    XRPL_TOKENS.save(deps.storage, key, &token)?;

    // Create the pending operation to approve the token again
    let config = CONFIG.load(deps.storage)?;
    let ticket = allocate_ticket(deps.storage)?;

    create_pending_operation(
        deps.storage,
        Some(ticket),
        None,
        OperationType::TrustSet {
            issuer: issuer.to_owned(),
            currency: currency.to_owned(),
            trust_set_limit_amount: config.trust_set_limit_amount,
        },
    )?;

    Ok(Response::new()
        .add_attribute(
            "action",
            ContractActions::RecoverXRPLTokenRegistration.as_str(),
        )
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency))
}

fn save_signature(
    deps: DepsMut,
    sender: Addr,
    operation_id: u64,
    signature: String,
) -> CoreumResult<ContractError> {
    assert_relayer(deps.as_ref(), sender.clone())?;

    add_signature(deps, operation_id, sender.clone(), signature.clone())?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::SaveSignature.as_str())
        .add_attribute("operation_id", operation_id.to_string())
        .add_attribute("relayer_address", sender.to_string())
        .add_attribute("signature", signature.as_str()))
}

fn send_to_xrpl(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    recipient: String,
) -> CoreumResult<ContractError> {
    // Check that we are only sending 1 type of coin
    let funds = one_coin(&info)?;

    // Check that the recipient is a valid XRPL address.
    validate_xrpl_address(recipient.to_owned())?;

    let mut response = Response::new();
    let decimals;
    let amount_to_send;
    let issuer;
    let currency;
    // We check if the token we are sending is an XRPL originated token or not
    match XRPL_TOKENS
        .idx
        .coreum_denom
        .item(deps.storage, funds.denom.to_owned())
        .map(|res| res.map(|pk_token| pk_token.1))?
    {
        // If it's an XRPL originated token we need to check that it's enabled and if it is apply the sending precision
        Some(xrpl_token) => {
            if xrpl_token.state.ne(&TokenState::Enabled) {
                return Err(ContractError::XRPLTokenNotEnabled {});
            }

            issuer = xrpl_token.issuer;
            currency = xrpl_token.currency;
            decimals = match is_token_xrp(issuer.to_owned(), currency.to_owned()) {
                true => XRP_DECIMALS,
                false => XRPL_TOKENS_DECIMALS,
            };

            // We don't need any decimal conversion because the token is an XRPL originated token and they are issued with same decimals
            amount_to_send = truncate_amount(xrpl_token.sending_precision, decimals, funds.amount)?;

            // Since tokens are being sent back we need to burn them in the contract
            // TODO(keyleu): for now we are BURNING the entire amount but when fees are implemented this will not happen and part of the amount will be burned and fees will be collected
            let burn_msg = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Burn {
                coin: coin(funds.amount.u128(), xrpl_token.coreum_denom),
            }));
            response = response.add_message(burn_msg);
        }

        None => {
            // If it's not an XRPL originated token we need to check that it's registered as a Coreum originated token
            let coreum_token = COREUM_TOKENS
                .load(deps.storage, funds.denom.to_owned())
                .map_err(|_| ContractError::TokenNotRegistered {})?;

            if coreum_token.state.ne(&TokenState::Enabled) {
                return Err(ContractError::CoreumOriginatedTokenDisabled {});
            }

            let config = CONFIG.load(deps.storage)?;

            decimals = coreum_token.decimals;
            issuer = config.bridge_xrpl_address;
            currency = coreum_token.xrpl_currency;

            // Since this is a Coreum originated token with different decimals, we are first going to truncate according to sending precision and then we will convert
            // to corresponding XRPL decimals.
            amount_to_send = truncate_and_convert_amount(
                coreum_token.sending_precision,
                decimals,
                XRPL_TOKENS_DECIMALS,
                funds.amount,
            )?;

            // For Coreum originated tokens we need to check that we are not going over max bridge amount.
            if deps
                .querier
                .query_balance(env.contract.address, coreum_token.denom)?
                .amount
                .gt(&coreum_token.max_holding_amount)
            {
                return Err(ContractError::MaximumBridgedAmountReached {});
            }
        }
    }

    // Get a ticket and store the pending operation
    let ticket = allocate_ticket(deps.storage)?;
    create_pending_operation(
        deps.storage,
        Some(ticket),
        None,
        OperationType::CoreumToXRPLTransfer {
            issuer,
            currency,
            amount: amount_to_send,
            recipient: recipient.to_owned(),
        },
    )?;

    Ok(response
        .add_attribute("action", ContractActions::SendToXRPL.as_str())
        .add_attribute("sender", info.sender)
        .add_attribute("recipient", recipient)
        .add_attribute("coin", funds.to_string()))
}

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => to_json_binary(&query_config(deps)?),
        QueryMsg::XRPLTokens { offset, limit } => {
            to_json_binary(&query_xrpl_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumTokens { offset, limit } => {
            to_json_binary(&query_coreum_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumTokenByXRPLCurrency { xrpl_currency } => {
            to_json_binary(&query_coreum_token_by_xrpl_currency(deps, xrpl_currency)?)
        }
        QueryMsg::Ownership {} => to_json_binary(&get_ownership(deps.storage)?),
        QueryMsg::PendingOperations {} => to_json_binary(&query_pending_operations(deps)?),
        QueryMsg::AvailableTickets {} => to_json_binary(&query_available_tickets(deps)?),
    }
}

fn query_config(deps: Deps) -> StdResult<Config> {
    let config = CONFIG.load(deps.storage)?;
    Ok(config)
}

fn query_xrpl_tokens(
    deps: Deps,
    offset: Option<u64>,
    limit: Option<u32>,
) -> StdResult<XRPLTokensResponse> {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let offset = offset.unwrap_or_default();
    let tokens: Vec<XRPLToken> = XRPL_TOKENS
        .range(deps.storage, None, None, Order::Ascending)
        .skip(offset as usize)
        .take(limit as usize)
        .filter_map(|v| v.ok())
        .map(|(_, v)| v)
        .collect();

    Ok(XRPLTokensResponse { tokens })
}

fn query_coreum_tokens(
    deps: Deps,
    offset: Option<u64>,
    limit: Option<u32>,
) -> StdResult<CoreumTokensResponse> {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let offset = offset.unwrap_or_default();
    let tokens: Vec<CoreumToken> = COREUM_TOKENS
        .range(deps.storage, None, None, Order::Ascending)
        .skip(offset as usize)
        .take(limit as usize)
        .filter_map(|r| r.ok())
        .map(|(_, ct)| ct)
        .collect();

    Ok(CoreumTokensResponse { tokens })
}

fn query_coreum_token_by_xrpl_currency(
    deps: Deps,
    xrpl_currency: String,
) -> StdResult<CoreumTokenResponse> {
    let token = COREUM_TOKENS
        .idx
        .xrpl_currency
        .item(deps.storage, xrpl_currency)?
        .map(|(_, ct)| ct);

    Ok(CoreumTokenResponse { token })
}

fn query_pending_operations(deps: Deps) -> StdResult<PendingOperationsResponse> {
    let operations: Vec<Operation> = PENDING_OPERATIONS
        .range(deps.storage, None, None, Order::Ascending)
        .filter_map(|v| v.ok())
        .map(|(_, v)| v)
        .collect();

    Ok(PendingOperationsResponse { operations })
}

fn query_available_tickets(deps: Deps) -> StdResult<AvailableTicketsResponse> {
    let mut tickets = AVAILABLE_TICKETS.load(deps.storage)?;

    Ok(AvailableTicketsResponse {
        tickets: tickets.make_contiguous().to_vec(),
    })
}

// ********** Helpers **********

fn check_issue_fee(deps: &DepsMut<CoreumQueries>, info: &MessageInfo) -> Result<(), ContractError> {
    let query_params_res: ParamsResponse = deps
        .querier
        .query(&CoreumQueries::AssetFT(Query::Params {}).into())?;

    if query_params_res.params.issue_fee != one_coin(info)? {
        return Err(ContractError::InvalidFundsAmount {});
    }

    Ok(())
}

pub fn build_xrpl_token_key(issuer: String, currency: String) -> String {
    // Issuer+currency is the key we use to find an XRPL
    let mut key = issuer;
    key.push_str(currency.as_str());
    key
}

pub fn validate_xrpl_issuer_and_currency(
    issuer: String,
    currency: String,
) -> Result<(), ContractError> {
    validate_xrpl_address(issuer).map_err(|_| ContractError::InvalidXRPLIssuer {})?;

    // We check that currency is either a standard 3 character currency or it's a 40 character hex string currency
    if !(currency.len() == 3 && currency.is_ascii()
        || currency.len() == 40 && currency.chars().all(|c| c.is_ascii_hexdigit()))
    {
        return Err(ContractError::InvalidXRPLCurrency {});
    }

    Ok(())
}

// Function used to truncate the amount to not send tokens over the sending precision.
fn truncate_amount(
    sending_precision: i32,
    decimals: u32,
    amount: Uint128,
) -> Result<Uint128, ContractError> {
    // To get exactly by how much we need to divide the original amount
    // Example: if sending precision = -1. Exponent will be 15 - (-1) = 16 for XRPL tokens so we will divide the original amount by 1e16
    // Example: if sending precision = 14. Exponent will be 15 - 14 = 1 for XRPL tokens so we will divide the original amount by 10
    let exponent = decimals as i32 - sending_precision;

    let amount_to_send = amount.checked_div(Uint128::new(10u128.pow(exponent.unsigned_abs())))?;

    if amount_to_send.is_zero() {
        return Err(ContractError::AmountSentIsZeroAfterTruncation {});
    }

    Ok(amount_to_send.checked_mul(Uint128::new(10u128.pow(exponent.unsigned_abs())))?)
}

// Function used to convert the amount received from XRPL with XRPL decimals to the Coreum amount with Coreum decimals
fn convert_amount_decimals(
    from_decimals: u32,
    to_decimals: u32,
    amount: Uint128,
) -> Result<Uint128, ContractError> {
    let converted_amount = match from_decimals.cmp(&to_decimals) {
        std::cmp::Ordering::Less => amount.checked_mul(Uint128::new(
            10u128.pow(to_decimals.saturating_sub(from_decimals)),
        ))?,
        std::cmp::Ordering::Greater => amount.checked_div(Uint128::new(
            10u128.pow(from_decimals.saturating_sub(to_decimals)),
        ))?,
        std::cmp::Ordering::Equal => amount,
    };

    Ok(converted_amount)
}

// Helper function to combine the conversion and truncation of amounts.
fn convert_and_truncate_amount(
    sending_precision: i32,
    from_decimals: u32,
    to_decimals: u32,
    amount: Uint128,
) -> Result<Uint128, ContractError> {
    let converted_amount = convert_amount_decimals(from_decimals, to_decimals, amount)?;
    let truncated_amount = truncate_amount(sending_precision, to_decimals, converted_amount)?;
    Ok(truncated_amount)
}

// Helper function to combine the truncation and conversion of amounts
fn truncate_and_convert_amount(
    sending_precision: i32,
    from_decimals: u32,
    to_decimals: u32,
    amount: Uint128,
) -> Result<Uint128, ContractError> {
    let truncated_amount = truncate_amount(sending_precision, from_decimals, amount)?;
    let converted_amount = convert_amount_decimals(from_decimals, to_decimals, truncated_amount)?;
    Ok(converted_amount)
}

fn is_token_xrp(issuer: String, currency: String) -> bool {
    issuer == XRP_ISSUER && currency == XRP_CURRENCY
}

fn convert_currency_to_xrpl_hexadecimal(currency: String) -> String {
    // Fill with zeros to get the correct hex representation in XRPL of our currency.
    format!("{:0<40}", hex::encode(currency))
}
