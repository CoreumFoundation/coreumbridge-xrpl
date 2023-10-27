use std::collections::VecDeque;

use crate::{
    error::ContractError,
    evidence::{handle_evidence, hash_bytes, Evidence, OperationResult, TransactionResult},
    msg::{
        AvailableTicketsResponse, CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg,
        InstantiateMsg, PendingOperationsResponse, QueryMsg, XRPLTokensResponse,
    },
    operation::{
        check_and_save_pending_operation, check_operation_exists, handle_trust_set_confirmation,
        Operation, OperationType,
    },
    relayer::{assert_relayer, validate_relayers, validate_xrpl_address},
    signatures::add_signature,
    state::{
        Config, ContractActions, CoreumToken, XRPLToken, AVAILABLE_TICKETS, CONFIG, COREUM_TOKENS,
        PENDING_OPERATIONS, PENDING_TICKET_UPDATE, USED_TICKETS_COUNTER,
        USED_XRPL_CURRENCIES_FOR_COREUM_TOKENS, XRPL_TOKENS,
    },
    tickets::{allocate_ticket, handle_ticket_allocation_confirmation, register_used_ticket},
};

use coreum_wasm_sdk::{
    assetft::{self, Msg::Issue, ParamsResponse, Query, BURNING, IBC, MINTING},
    core::{CoreumMsg, CoreumQueries, CoreumResult},
};
use cosmwasm_std::{
    coin, coins, entry_point, to_binary, Addr, Binary, CosmosMsg, Deps, DepsMut, Empty, Env,
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

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;

    //Threshold can't be more than number of relayers
    if msg.evidence_threshold > msg.relayers.len().try_into().unwrap() {
        return Err(ContractError::InvalidThreshold {});
    }

    //We need to allow at least 2 tickets and less than 250 (XRPL limit) to be used
    if msg.used_tickets_threshold <= 1 || msg.used_tickets_threshold > MAX_TICKETS {
        return Err(ContractError::InvalidUsedTicketsThreshold {});
    }

    //We initialize these values here so that we can immediately start working with them
    USED_TICKETS_COUNTER.save(deps.storage, &0)?;
    PENDING_TICKET_UPDATE.save(deps.storage, &false)?;
    AVAILABLE_TICKETS.save(deps.storage, &VecDeque::new())?;

    let config = Config {
        relayers: msg.relayers,
        evidence_threshold: msg.evidence_threshold,
        used_tickets_threshold: msg.used_tickets_threshold,
        trust_set_limit_amount: msg.trust_set_limit_amount,
    };

    CONFIG.save(deps.storage, &config)?;

    let xrp_issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: XRP_SYMBOL.to_string(),
        subunit: XRP_SUBUNIT.to_string(),
        precision: XRP_DECIMALS,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, BURNING, IBC]),
        burn_rate: Some("0.0".to_string()),
        send_commission_rate: Some("0.0".to_string()),
    }));

    let xrp_in_coreum = format!("{}-{}", XRP_SUBUNIT, env.contract.address).to_lowercase();

    //We save the link between the denom in the Coreum chain and the denom in XRPL, so that when we receive
    //a token we can inform the relayers of what is being sent back.
    let token = XRPLToken {
        issuer: XRP_ISSUER.to_string(),
        currency: XRP_CURRENCY.to_string(),
        coreum_denom: xrp_in_coreum,
        sending_precision: XRP_DEFAULT_SENDING_PRECISION,
        max_holding_amount: Uint128::new(XRP_DEFAULT_MAX_HOLDING_AMOUNT),
        //The XRP active token will be active from the start because it doesn't need approval to be received by the multisig
        active: true,
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
        ExecuteMsg::RegisterCoreumToken { denom, decimals } => {
            register_coreum_token(deps.into_empty(), env, denom, decimals, info.sender)
        }
        ExecuteMsg::RegisterXRPLToken {
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
        } => register_xrpl_token(
            deps,
            env,
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
            info,
        ),
        ExecuteMsg::SaveEvidence { evidence } => {
            save_evidence(deps.into_empty(), info.sender, evidence)
        }
        ExecuteMsg::RecoverTickets {
            sequence_number,
            number_of_tickets,
        } => recover_tickets(
            deps.into_empty(),
            info.sender,
            sequence_number,
            number_of_tickets,
        ),
        ExecuteMsg::RegisterSignature {
            operation_id,
            signature,
        } => register_signature(deps.into_empty(), info.sender, operation_id, signature),
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
    denom: String,
    decimals: u32,
    sender: Addr,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &sender)?;

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

    //format will be the hex representation in XRPL of the string coreum<hash>
    let xrpl_currency =
        convert_currency_to_xrpl_hexadecimal(format!("{}{}", COREUM_CURRENCY_PREFIX, hex_string));

    if USED_XRPL_CURRENCIES_FOR_COREUM_TOKENS.has(deps.storage, xrpl_currency.clone()) {
        return Err(ContractError::RegistrationFailure {});
    }
    USED_XRPL_CURRENCIES_FOR_COREUM_TOKENS.save(deps.storage, xrpl_currency.clone(), &Empty {})?;

    let token = CoreumToken {
        denom: denom.clone(),
        decimals,
        xrpl_currency: xrpl_currency.clone(),
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
    issuer: String,
    currency: String,
    sending_precision: i32,
    max_holding_amount: Uint128,
    info: MessageInfo,
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
        burn_rate: Some("0.0".to_string()),
        send_commission_rate: Some("0.0".to_string()),
    }));

    // Denom that token will have in Coreum
    let denom = format!("{}-{}", symbol_and_subunit, env.contract.address).to_lowercase();

    // This in theory is not necessary because issue_msg would fail if the denom already exists but it's a double check and a wait to return a more readable error.
    if COREUM_TOKENS.has(deps.storage, denom.clone()) {
        return Err(ContractError::RegistrationFailure {});
    };

    let token = XRPLToken {
        issuer: issuer.clone(),
        currency: currency.clone(),
        coreum_denom: denom.clone(),
        sending_precision,
        max_holding_amount,
        // Registered tokens will start being inactive until they are approved
        active: false,
    };

    XRPL_TOKENS.save(deps.storage, key, &token)?;

    //Create the pending operation to approve the token
    let config = CONFIG.load(deps.storage)?;
    let ticket = allocate_ticket(deps.storage)?;
    check_and_save_pending_operation(
        deps.storage,
        ticket,
        &Operation {
            ticket_number: Some(ticket),
            sequence_number: None,
            signatures: vec![],
            operation_type: OperationType::TrustSet {
                issuer: issuer.clone(),
                currency: currency.clone(),
                trust_set_limit_amount: config.trust_set_limit_amount,
            },
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
    evidence.validate()?;

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
            //Create issuer+currency key to find denom on coreum.
            let key = build_xrpl_token_key(issuer.clone(), currency.clone());

            let token = XRPL_TOKENS
                .load(deps.storage, key)
                .map_err(|_| ContractError::TokenNotRegistered {})?;

            if !token.active {
                return Err(ContractError::XRPLTokenNotActive {});
            }

            let decimals = match is_token_xrp(token.issuer, token.currency) {
                true => XRP_DECIMALS,
                false => XRPL_TOKENS_DECIMALS,
            };

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
                response = add_mint_and_send(
                    response,
                    amount_to_send,
                    token.coreum_denom,
                    recipient.clone(),
                );
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
            sequence_number,
            ticket_number,
            transaction_result,
            operation_result,
        } => {
            let operation_id =
                check_operation_exists(deps.storage, sequence_number, ticket_number)?;

            if threshold_reached {
                match operation_result.clone() {
                    OperationResult::TicketsAllocation { tickets } => {
                        handle_ticket_allocation_confirmation(
                            deps.storage,
                            tickets,
                            transaction_result.clone(),
                        )?;
                    }
                    OperationResult::TrustSet { issuer, currency } => {
                        handle_trust_set_confirmation(deps.storage, issuer, currency, transaction_result.clone())?;
                    }
                }
                PENDING_OPERATIONS.remove(deps.storage, operation_id);

                if transaction_result.ne(&TransactionResult::Invalid) && ticket_number.is_some() {
                    register_used_ticket(deps.storage)?
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
    sequence_number: u64,
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
    //If we don't provide a number of tickets to recover we will recover the ones that we already used.
    let number_to_allocate = number_of_tickets.unwrap_or(used_tickets);

    if number_to_allocate == 0 || number_to_allocate > MAX_TICKETS {
        return Err(ContractError::InvalidTicketNumberToAllocate {});
    }

    check_and_save_pending_operation(
        deps.storage,
        sequence_number,
        &Operation {
            ticket_number: None,
            sequence_number: Some(sequence_number),
            signatures: vec![],
            operation_type: OperationType::AllocateTickets {
                number: number_to_allocate,
            },
        },
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RecoverTickets.as_str())
        .add_attribute("sequence_number", sequence_number.to_string()))
}

fn register_signature(
    deps: DepsMut,
    sender: Addr,
    operation_id: u64,
    signature: String,
) -> CoreumResult<ContractError> {
    assert_relayer(deps.as_ref(), sender.clone())?;

    add_signature(deps, operation_id, sender.clone(), signature.clone())?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RegisterSignature.as_str())
        .add_attribute("operation_id", operation_id.to_string())
        .add_attribute("relayer_address", sender.to_string())
        .add_attribute("signature", signature.as_str()))
}

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => to_binary(&query_config(deps)?),
        QueryMsg::XRPLTokens { offset, limit } => {
            to_binary(&query_xrpl_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumTokens { offset, limit } => {
            to_binary(&query_coreum_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumToken { denom } => to_binary(&query_coreum_token(deps, denom)?),
        QueryMsg::Ownership {} => to_binary(&get_ownership(deps.storage)?),
        QueryMsg::PendingOperations {} => to_binary(&query_pending_operations(deps)?),
        QueryMsg::AvailableTickets {} => to_binary(&query_available_tickets(deps)?),
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
    let offset = offset.unwrap_or(0);
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
    let offset = offset.unwrap_or(0);
    let tokens: Vec<CoreumToken> = COREUM_TOKENS
        .range(deps.storage, None, None, Order::Ascending)
        .skip(offset as usize)
        .take(limit as usize)
        .filter_map(|v| v.ok())
        .map(|(_, v)| v)
        .collect();

    Ok(CoreumTokensResponse { tokens })
}

fn query_coreum_token(deps: Deps, denom: String) -> StdResult<CoreumTokenResponse> {
    let token = COREUM_TOKENS.load(deps.storage, denom)?;

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

//TODO: In the future we might have a way to do this in one instruction.
fn add_mint_and_send(
    response: Response<CoreumMsg>,
    amount: Uint128,
    denom: String,
    recipient: Addr,
) -> Response<CoreumMsg> {
    let mint_msg = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Mint {
        coin: coin(amount.u128(), denom.clone()),
    }));

    let send_msg = CosmosMsg::Bank(cosmwasm_std::BankMsg::Send {
        to_address: recipient.to_string(),
        amount: coins(amount.u128(), denom),
    });

    response.add_messages([mint_msg, send_msg])
}

pub fn build_xrpl_token_key(issuer: String, currency: String) -> String {
    //issuer+currency is the key we use to find an XRPL
    let mut key = issuer;
    key.push_str(currency.as_str());
    key
}

pub fn validate_xrpl_issuer_and_currency(
    issuer: String,
    currency: String,
) -> Result<(), ContractError> {
    validate_xrpl_address(issuer).map_err(|_| ContractError::InvalidXRPLIssuer {})?;

    //We check that currency is either a standard 3 character currency or it's a 40 character hex string currency
    if !(currency.len() == 3 && currency.is_ascii()
        || currency.len() == 40 && currency.chars().all(|c| c.is_ascii_hexdigit()))
    {
        return Err(ContractError::InvalidXRPLCurrency {});
    }

    Ok(())
}

fn truncate_amount(
    sending_precision: i32,
    decimals: u32,
    amount: Uint128,
) -> Result<Uint128, ContractError> {
    // To get exactly by how much we need to divide the original amount
    // Example: if sending precision = -1. Exponent will be 15 - ( - 1) = 16 for XRPL tokens so we will divide the original amount by 10^16
    // Example: if sending precision = 14. Exponent will be 15 - 14 = 1 for XRPL tokens so we will divide the original amount by 10^1
    let exponent = decimals as i32 - sending_precision;

    let amount_to_send = amount.checked_div(Uint128::new(10u128.pow(exponent.unsigned_abs())))?;

    if amount_to_send.is_zero() {
        return Err(ContractError::AmountSentIsZeroAfterTruncating {});
    }

    Ok(amount_to_send.checked_mul(Uint128::new(10u128.pow(exponent.unsigned_abs())))?)
}

fn is_token_xrp(issuer: String, currency: String) -> bool {
    issuer == XRP_ISSUER && currency == XRP_CURRENCY
}

fn convert_currency_to_xrpl_hexadecimal(currency: String) -> String {
    //Fill with zeros to get the correct hex representation in XRPL of our currency.
    format!("{:0<40}", hex::encode(currency))
}
