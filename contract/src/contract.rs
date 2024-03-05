use std::collections::VecDeque;

use crate::{
    address::validate_xrpl_address,
    error::ContractError,
    evidence::OperationResult::TicketsAllocation,
    evidence::{handle_evidence, hash_bytes, Evidence, TransactionResult},
    fees::{amount_after_bridge_fees, handle_fee_collection, substract_relayer_fees},
    msg::{
        AvailableTicketsResponse, BridgeStateResponse, CoreumTokensResponse, ExecuteMsg,
        FeesCollectedResponse, InstantiateMsg, PendingOperationsResponse, PendingRefund,
        PendingRefundsResponse, ProcessedTxsResponse, ProhibitedXRPLAddressesResponse, QueryMsg,
        TransactionEvidence, TransactionEvidencesResponse, XRPLTokensResponse,
    },
    operation::{
        check_operation_exists, create_pending_operation, handle_operation, remove_pending_refund,
        Operation, OperationType,
    },
    relayer::{is_relayer, validate_relayers, Relayer},
    signatures::add_signature,
    state::{
        BridgeState, Config, ContractActions, CoreumToken, TokenState, UserType, XRPLToken,
        AVAILABLE_TICKETS, CONFIG, COREUM_TOKENS, FEES_COLLECTED, PENDING_OPERATIONS,
        PENDING_REFUNDS, PENDING_ROTATE_KEYS, PENDING_TICKET_UPDATE, PROCESSED_TXS,
        PROHIBITED_XRPL_ADDRESSES, TX_EVIDENCES, USED_TICKETS_COUNTER, XRPL_TOKENS,
    },
    tickets::{allocate_ticket, register_used_ticket},
    token::{
        build_xrpl_token_key, is_token_xrp, set_token_bridging_fee, set_token_max_holding_amount,
        set_token_sending_precision, set_token_state,
    },
};

use coreum_wasm_sdk::{
    assetft::{self, Msg::Issue, ParamsResponse, Query, IBC, MINTING},
    core::{CoreumMsg, CoreumQueries, CoreumResult},
};
use cosmwasm_std::{
    coin, coins, entry_point, to_json_binary, Addr, BankMsg, Binary, Coin, CosmosMsg, Deps,
    DepsMut, Empty, Env, MessageInfo, Order, Response, StdResult, Storage, Uint128,
};
use cw2::set_contract_version;
use cw_ownable::{get_ownership, initialize_owner, is_owner, Action};
use cw_storage_plus::Bound;
use cw_utils::one_coin;

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

// pagination info for queries
const MAX_PAGE_LIMIT: u32 = 250;

// Range of precisions that can be used for tokens
const MIN_SENDING_PRECISION: i32 = -15;
const MAX_SENDING_PRECISION: i32 = 15;

// Maximum amount of decimals a Coreum token can be registered with
pub const MAX_COREUM_TOKEN_DECIMALS: u32 = 100;

pub const MAX_TICKETS: u32 = 250;
pub const MAX_RELAYERS: usize = 32;

// Information for the XRP token
const XRP_SYMBOL: &str = "XRP";
const XRP_SUBUNIT: &str = "drop";
const XRP_DECIMALS: u32 = 6;
pub const XRP_CURRENCY: &str = "XRP";
pub const XRP_ISSUER: &str = "rrrrrrrrrrrrrrrrrrrrrhoLvTp";
const XRP_DEFAULT_SENDING_PRECISION: i32 = 6;
const XRP_DEFAULT_MAX_HOLDING_AMOUNT: u128 =
    10u128.pow(16 - XRP_DEFAULT_SENDING_PRECISION as u32 + XRP_DECIMALS);
const XRP_DEFAULT_FEE: Uint128 = Uint128::zero();

const COREUM_CURRENCY_PREFIX: &str = "coreum";
const XRPL_DENOM_PREFIX: &str = "xrpl";

const ALLOWED_CURRENCY_SYMBOLS: [char; 18] = [
    '?', '!', '@', '#', '$', '%', '^', '&', '*', '<', '>', '(', ')', '{', '}', '[', ']', '|',
];

// All XRPL originated tokens (except XRP) have 15 decimals
pub const XRPL_TOKENS_DECIMALS: u32 = 15;
// A valid XRPL amount is one that doesn't have more than 16 digits after trimming trailing zeroes
pub const XRPL_MAX_TRUNCATED_AMOUNT_LENGTH: usize = 16;

pub const MIN_DENOM_LENGTH: usize = 3;
pub const MAX_DENOM_LENGTH: usize = 128;
pub const DENOM_SPECIAL_CHARACTERS: [char; 5] = ['/', ':', '.', '_', '-'];

pub const INITIAL_PROHIBITED_XRPL_ADDRESSES: [&str; 5] = [
    "rrrrrrrrrrrrrrrrrrrrrhoLvTp", // ACCOUNT_ZERO: An address that is the XRP Ledger's base58 encoding of the value 0. In peer-to-peer communications, rippled uses this address as the issuer for XRP.
    "rrrrrrrrrrrrrrrrrrrrBZbvji", // ACCOUNT_ONE: An address that is the XRP Ledger's base58 encoding of the value 1. In the ledger, RippleState entries use this address as a placeholder for the issuer of a trust line balance.
    "rHb9CJAWyB4rj91VRWn96DkukG4bwdtyTh", // Genesis account: When rippled starts a new genesis ledger from scratch (for example, in stand-alone mode), this account holds all the XRP. This address is generated from the seed value masterpassphrase which is hard-coded.
    "rrrrrrrrrrrrrrrrrNAMEtxvNvQ", // Ripple Name reservation black-hole: In the past, Ripple asked users to send XRP to this account to reserve Ripple Names.
    "rrrrrrrrrrrrrrrrrrrn5RM1rHd", // NaN Address: Previous versions of ripple-lib generated this address when encoding the value NaN using the XRP Ledger's base58 string encoding format.
];

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

    validate_relayers(
        deps.as_ref().into_empty(),
        &msg.relayers,
        msg.evidence_threshold,
    )?;

    // The multisig address on XRPL must be valid
    validate_xrpl_address(&msg.bridge_xrpl_address)?;

    // We want to check that exactly the issue fee was sent
    check_issue_fee(&deps, &info)?;

    // We need to allow at least 2 tickets and less or equal than 250 (XRPL limit) to be used before triggering a ticket allocation action
    if msg.used_ticket_sequence_threshold <= 1 || msg.used_ticket_sequence_threshold > MAX_TICKETS {
        return Err(ContractError::InvalidUsedTicketSequenceThreshold {});
    }

    // We validate the trust set amount is a valid XRPL amount
    validate_xrpl_amount(msg.trust_set_limit_amount)?;

    // We initialize these values here so that we can immediately start working with them
    USED_TICKETS_COUNTER.save(deps.storage, &0)?;
    PENDING_TICKET_UPDATE.save(deps.storage, &false)?;
    PENDING_ROTATE_KEYS.save(deps.storage, &false)?;
    AVAILABLE_TICKETS.save(deps.storage, &VecDeque::new())?;

    let config = Config {
        relayers: msg.relayers,
        evidence_threshold: msg.evidence_threshold,
        used_ticket_sequence_threshold: msg.used_ticket_sequence_threshold,
        trust_set_limit_amount: msg.trust_set_limit_amount,
        bridge_xrpl_address: msg.bridge_xrpl_address.clone(),
        bridge_state: BridgeState::Active,
        xrpl_base_fee: msg.xrpl_base_fee,
    };

    CONFIG.save(deps.storage, &config)?;

    // We will issue the XRP token during instantiation. We don't need to register it
    let xrp_issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: XRP_SYMBOL.to_string(),
        subunit: XRP_SUBUNIT.to_string(),
        precision: XRP_DECIMALS,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, IBC]),
        burn_rate: "0.0".to_string(),
        send_commission_rate: "0.0".to_string(),
        uri: None,
        uri_hash: None,
    }));

    let xrp_coreum_denom = format!("{}-{}", XRP_SUBUNIT, env.contract.address).to_lowercase();

    // We store the representation of XRP in our XRPLTokens list using the issuer+currency as key
    let token = XRPLToken {
        issuer: XRP_ISSUER.to_string(),
        currency: XRP_CURRENCY.to_string(),
        coreum_denom: xrp_coreum_denom,
        sending_precision: XRP_DEFAULT_SENDING_PRECISION,
        max_holding_amount: Uint128::new(XRP_DEFAULT_MAX_HOLDING_AMOUNT),
        // The XRP token is enabled from the start because it doesn't need approval to be received on the XRPL side
        state: TokenState::Enabled,
        bridging_fee: XRP_DEFAULT_FEE,
    };

    let key = build_xrpl_token_key(XRP_ISSUER, XRP_CURRENCY);
    XRPL_TOKENS.save(deps.storage, key, &token)?;

    // We store all the prohibited addresses in state, including the multisig address, which is also prohibited to send to
    for address in INITIAL_PROHIBITED_XRPL_ADDRESSES {
        PROHIBITED_XRPL_ADDRESSES.save(deps.storage, address.to_string(), &Empty {})?;
    }
    PROHIBITED_XRPL_ADDRESSES.save(deps.storage, msg.bridge_xrpl_address, &Empty {})?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::Instantiation.as_str())
        .add_attribute("contract_name", CONTRACT_NAME)
        .add_attribute("contract_version", CONTRACT_VERSION)
        .add_attribute("owner", msg.owner)
        .add_attribute("sender", info.sender)
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
            bridging_fee,
        } => register_coreum_token(
            deps.into_empty(),
            env,
            info.sender,
            denom,
            decimals,
            sending_precision,
            max_holding_amount,
            bridging_fee,
        ),
        ExecuteMsg::RegisterXRPLToken {
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
            bridging_fee,
        } => register_xrpl_token(
            deps,
            env,
            info,
            issuer,
            currency,
            sending_precision,
            max_holding_amount,
            bridging_fee,
        ),
        ExecuteMsg::SaveEvidence { evidence } => {
            save_evidence(deps.into_empty(), env, info.sender, evidence)
        }
        ExecuteMsg::RecoverTickets {
            account_sequence,
            number_of_tickets,
        } => recover_tickets(
            deps.into_empty(),
            env.block.time.seconds(),
            info.sender,
            account_sequence,
            number_of_tickets,
        ),
        ExecuteMsg::RecoverXRPLTokenRegistration { issuer, currency } => {
            recover_xrpl_token_registration(
                deps.into_empty(),
                env.block.time.seconds(),
                info.sender,
                issuer,
                currency,
            )
        }
        ExecuteMsg::SaveSignature {
            operation_id,
            operation_version,
            signature,
        } => save_signature(
            deps.into_empty(),
            info.sender,
            operation_id,
            operation_version,
            &signature,
        ),
        ExecuteMsg::SendToXRPL {
            recipient,
            deliver_amount,
        } => send_to_xrpl(deps.into_empty(), env, info, recipient, deliver_amount),
        ExecuteMsg::UpdateXRPLToken {
            issuer,
            currency,
            state,
            sending_precision,
            bridging_fee,
            max_holding_amount,
        } => update_xrpl_token(
            deps.into_empty(),
            info.sender,
            issuer,
            currency,
            state,
            sending_precision,
            bridging_fee,
            max_holding_amount,
        ),
        ExecuteMsg::UpdateCoreumToken {
            denom,
            state,
            sending_precision,
            bridging_fee,
            max_holding_amount,
        } => update_coreum_token(
            deps.into_empty(),
            env,
            info.sender,
            denom,
            state,
            sending_precision,
            bridging_fee,
            max_holding_amount,
        ),
        ExecuteMsg::UpdateXRPLBaseFee { xrpl_base_fee } => {
            update_xrpl_base_fee(deps.into_empty(), info.sender, xrpl_base_fee)
        }
        ExecuteMsg::ClaimRefund { pending_refund_id } => {
            claim_pending_refund(deps.into_empty(), info.sender, pending_refund_id)
        }
        ExecuteMsg::ClaimRelayerFees { amounts } => {
            claim_relayer_fees(deps.into_empty(), info.sender, amounts)
        }
        ExecuteMsg::HaltBridge {} => halt_bridge(deps.into_empty(), info.sender),
        ExecuteMsg::ResumeBridge {} => resume_bridge(deps.into_empty(), info.sender),
        ExecuteMsg::RotateKeys {
            new_relayers,
            new_evidence_threshold,
        } => rotate_keys(
            deps.into_empty(),
            env,
            info.sender,
            new_relayers,
            new_evidence_threshold,
        ),
        ExecuteMsg::UpdateProhibitedXRPLAddresses {
            prohibited_xrpl_addresses,
        } => update_prohibited_xrpl_addresses(
            deps.into_empty(),
            info.sender,
            prohibited_xrpl_addresses,
        ),
        ExecuteMsg::CancelPendingOperation { operation_id } => {
            cancel_pending_operation(deps.into_empty(), info.sender, operation_id)
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
    Ok(Response::new()
        .add_attribute("sender", info.sender)
        .add_attributes(ownership.into_attributes()))
}

#[allow(clippy::too_many_arguments)]
fn register_coreum_token(
    deps: DepsMut,
    env: Env,
    sender: Addr,
    denom: String,
    decimals: u32,
    sending_precision: i32,
    max_holding_amount: Uint128,
    bridging_fee: Uint128,
) -> CoreumResult<ContractError> {
    check_authorization(deps.storage, &sender, &ContractActions::RegisterCoreumToken)?;
    assert_bridge_active(deps.as_ref())?;

    validate_coreum_token_decimals(decimals)?;
    validate_sending_precision(sending_precision, decimals)?;

    if COREUM_TOKENS.has(deps.storage, denom.clone()) {
        return Err(ContractError::CoreumTokenAlreadyRegistered { denom });
    }

    validate_coreum_denom(&denom)?;

    // We generate a currency creating a Sha256 hash of the denom, the decimals and the current time so that if it fails we can try again
    let to_hash = format!("{}{}{}", denom, decimals, env.block.time.seconds()).into_bytes();
    let hex_string = hash_bytes(to_hash)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    // Format will be the hex representation in XRPL of the string coreum<hash> in uppercase
    let xrpl_currency =
        convert_currency_to_xrpl_hexadecimal(format!("{COREUM_CURRENCY_PREFIX}{hex_string}"));

    // Validate XRPL currency just in case we got an invalid XRPL currency (starting with 0x00)
    validate_xrpl_currency(&xrpl_currency)?;

    // We check that the this currency is not used already (we got the same hash)
    if COREUM_TOKENS
        .idx
        .xrpl_currency
        .item(deps.storage, xrpl_currency.clone())?
        .is_some()
    {
        return Err(ContractError::RegistrationFailure {});
    }

    let token = CoreumToken {
        denom: denom.clone(),
        decimals,
        xrpl_currency: xrpl_currency.clone(),
        sending_precision,
        max_holding_amount,
        // All registered Coreum originated tokens will start as enabled because they don't need a TrustSet operation to be bridged because issuer for such tokens is bridge address
        state: TokenState::Enabled,
        bridging_fee,
    };
    COREUM_TOKENS.save(deps.storage, denom.clone(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RegisterCoreumToken.as_str())
        .add_attribute("sender", sender)
        .add_attribute("denom", denom)
        .add_attribute("decimals", decimals.to_string())
        .add_attribute("xrpl_currency_for_denom", xrpl_currency))
}

#[allow(clippy::too_many_arguments)]
fn register_xrpl_token(
    deps: DepsMut<CoreumQueries>,
    env: Env,
    info: MessageInfo,
    issuer: String,
    currency: String,
    sending_precision: i32,
    max_holding_amount: Uint128,
    bridging_fee: Uint128,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &info.sender,
        &ContractActions::RegisterXRPLToken,
    )?;

    validate_xrpl_address(&issuer)?;
    validate_xrpl_currency(&currency)?;

    validate_sending_precision(sending_precision, XRPL_TOKENS_DECIMALS)?;

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;
    let key = build_xrpl_token_key(&issuer, &currency);

    if XRPL_TOKENS.has(deps.storage, key.clone()) {
        return Err(ContractError::XRPLTokenAlreadyRegistered { issuer, currency });
    }

    // We check that the issuer is not prohibited
    check_address_is_prohibited(deps.storage, issuer.clone())?;

    // We generate a denom creating a Sha256 hash of the issuer, currency and current time
    let to_hash = format!("{}{}{}", issuer, currency, env.block.time.seconds()).into_bytes();

    // We encode the hash in hexadecimal and take the first 10 characters
    let hex_string = hash_bytes(to_hash)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    // Symbol and subunit we will use for the issued token in Coreum
    let symbol_and_subunit = format!("{XRPL_DENOM_PREFIX}{hex_string}");

    let issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: symbol_and_subunit.to_uppercase(),
        subunit: symbol_and_subunit.clone(),
        precision: XRPL_TOKENS_DECIMALS,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, IBC]),
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
        bridging_fee,
    };

    XRPL_TOKENS.save(deps.storage, key, &token)?;

    // Create the pending operation to approve the token
    let config = CONFIG.load(deps.storage)?;
    let ticket = allocate_ticket(deps.storage)?;

    // We create the TrustSet operation. If this operation is accepted, the token will be enabled, if not, it will be in Inactive state
    // waiting for owner to recover this operation
    create_pending_operation(
        deps.storage,
        env.block.time.seconds(),
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
        .add_attribute("sender", info.sender)
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency)
        .add_attribute("denom", denom))
}

fn save_evidence(
    deps: DepsMut,
    env: Env,
    sender: Addr,
    evidence: Evidence,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::SaveEvidence,
    )?;
    // Evidences can only be sent under 2 conditions:
    // 1. The bridge is active -> All evidences are accepted
    // 2. The bridge is halted -> Only ticket allocation and rotate keys evidences (if there is a rotate keys ongoing) are allowed
    let config = CONFIG.load(deps.storage)?;

    evidence.validate_basic()?;

    let threshold_reached = handle_evidence(deps.storage, sender.clone(), &evidence)?;

    let mut response = Response::new()
        .add_attribute("action", ContractActions::SaveEvidence.as_str())
        .add_attribute("sender", sender);

    match evidence {
        Evidence::XRPLToCoreumTransfer {
            tx_hash,
            issuer,
            currency,
            amount,
            recipient,
        } => {
            if config.bridge_state == BridgeState::Halted {
                return Err(ContractError::BridgeHalted {});
            }
            deps.api.addr_validate(recipient.as_ref())?;

            // If the recipient of the operation is the bridge contract address, we error
            if recipient.eq(&env.contract.address) {
                return Err(ContractError::ProhibitedAddress {});
            }

            // This means the token is not a Coreum originated token (the issuer is not the XRPL multisig address)
            if issuer.ne(&config.bridge_xrpl_address) {
                // Create issuer+currency key to find denom on coreum.
                let key = build_xrpl_token_key(&issuer, &currency);

                // To transfer a token it must be registered and activated
                let token = XRPL_TOKENS
                    .load(deps.storage, key)
                    .map_err(|_| ContractError::TokenNotRegistered {})?;

                if token.state.ne(&TokenState::Enabled) {
                    return Err(ContractError::TokenNotEnabled {});
                }

                let decimals = if is_token_xrp(&token.issuer, &token.currency) {
                    XRP_DECIMALS
                } else {
                    XRPL_TOKENS_DECIMALS
                };

                // We calculate the amount to send after applying the bridging fees for that token
                let amount_after_bridge_fees =
                    amount_after_bridge_fees(amount, token.bridging_fee)?;

                // Here we simply truncate because the Coreum tokens corresponding to XRPL originated tokens have the same decimals as their corresponding Coreum tokens
                let (amount_to_send, remainder) =
                    truncate_amount(token.sending_precision, decimals, amount_after_bridge_fees)?;

                // The amount the bridge can mint cannot exceed the max_holding_amount
                if amount
                    .checked_add(
                        deps.querier
                            .query_supply(token.coreum_denom.clone())?
                            .amount,
                    )?
                    .gt(&token.max_holding_amount)
                {
                    return Err(ContractError::MaximumBridgedAmountReached {});
                }

                // If enough evidences are provided (threshold reached), we collect fees and mint the token for the recipient
                if threshold_reached {
                    let fee_collected = handle_fee_collection(
                        deps.storage,
                        token.bridging_fee,
                        token.coreum_denom.clone(),
                        remainder,
                    )?;

                    let mint_msg_fees = CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Mint {
                        coin: coin(fee_collected.u128(), token.coreum_denom.clone()),
                        recipient: None,
                    }));

                    let mint_msg_for_recipient =
                        CosmosMsg::from(CoreumMsg::AssetFT(assetft::Msg::Mint {
                            coin: coin(amount_to_send.u128(), token.coreum_denom),
                            recipient: Some(recipient.to_string()),
                        }));

                    response = response.add_messages([mint_msg_fees, mint_msg_for_recipient]);
                }
            } else {
                // We check that the token is registered and enabled
                let token = match COREUM_TOKENS
                    .idx
                    .xrpl_currency
                    .item(deps.storage, currency.clone())?
                    .map(|(_, ct)| ct)
                {
                    Some(token) => {
                        if token.state.ne(&TokenState::Enabled) {
                            return Err(ContractError::TokenNotEnabled {});
                        }
                        token
                    }
                    // In practice this will never happen because any token issued from the multisig address is a token that was bridged from Coreum so it will be registered.
                    // This could theoretically happen if relayers agree and sign a transaction outside of bridge flow
                    None => return Err(ContractError::TokenNotRegistered {}),
                };

                // We first convert the amount we receive with XRPL decimals to the corresponding decimals in Coreum and then we apply the truncation according to sending precision
                let (amount_to_send, remainder) = convert_and_truncate_amount(
                    token.sending_precision,
                    XRPL_TOKENS_DECIMALS,
                    token.decimals,
                    amount,
                    token.bridging_fee,
                )?;

                // If enough evidences are provided (threshold reached), we collect fees and send tokens from the bridge contract (it was holding them in escrow)
                if threshold_reached {
                    handle_fee_collection(
                        deps.storage,
                        token.bridging_fee,
                        token.denom.clone(),
                        remainder,
                    )?;

                    let send_msg = BankMsg::Send {
                        to_address: recipient.to_string(),
                        amount: coins(amount_to_send.u128(), token.denom),
                    };
                    response = response.add_message(send_msg);
                }
            }

            response = response
                .add_attribute("hash", tx_hash)
                .add_attribute("issuer", issuer)
                .add_attribute("currency", currency)
                .add_attribute("amount", amount.to_string())
                .add_attribute("recipient", recipient.to_string())
                .add_attribute("threshold_reached", threshold_reached.to_string());
        }
        Evidence::XRPLTransactionResult {
            tx_hash,
            account_sequence,
            ticket_sequence,
            transaction_result,
            operation_result,
        } => {
            // An XRPL transaction uses an account sequence or a ticket sequence, but not both
            let operation_id = account_sequence.unwrap_or_else(|| ticket_sequence.unwrap());
            let operation = check_operation_exists(deps.storage, operation_id)?;

            // Validation for certain operation types that can't have account sequences
            match &operation.operation_type {
                // A TrustSet operation or CoreumToXRPLTransfer operation are only executed with tickets
                OperationType::TrustSet { .. } | OperationType::CoreumToXRPLTransfer { .. } => {
                    if account_sequence.is_some() {
                        return Err(ContractError::InvalidTransactionResultEvidence {});
                    }
                }
                _ => (),
            }

            // If enough evidences are provided (threshold reached), we run the specific handler for each operation
            if threshold_reached {
                // We run the handler for the operation, routing to the correct handler for each operation type
                handle_operation(
                    deps.storage,
                    &operation,
                    &operation_result,
                    &transaction_result,
                    &tx_hash,
                    operation_id,
                    ticket_sequence,
                    &mut response,
                )?;

                // If the operation was not Invalid, we must register a used ticket
                if transaction_result.ne(&TransactionResult::Invalid) && ticket_sequence.is_some() {
                    // If the operation must trigger a new ticket allocation we must know if we can trigger it
                    // or not (if we have tickets available). Therefore we will return a false flag if
                    // we don't have available tickets left and we will notify with an attribute.
                    // NOTE: This will only happen in the particular case of a rejected ticket allocation
                    // operation.
                    if !register_used_ticket(deps.storage, env.block.time.seconds())? {
                        response = response.add_attribute(
                            "adding_ticket_allocation_operation_success",
                            false.to_string(),
                        );
                    }
                }
            }

            response = response
                .add_attribute("operation_type", operation.operation_type.as_str())
                .add_attribute("operation_id", operation_id.to_string())
                .add_attribute("transaction_result", transaction_result.as_str())
                .add_attribute("threshold_reached", threshold_reached.to_string());

            if let Some(tx_hash) = tx_hash {
                response = response.add_attribute("tx_hash", tx_hash);
            }
        }
    }

    Ok(response)
}

fn recover_tickets(
    deps: DepsMut,
    timestamp: u64,
    sender: Addr,
    account_sequence: u64,
    number_of_tickets: Option<u32>,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::RecoverTickets,
    )?;

    let available_tickets = AVAILABLE_TICKETS.load(deps.storage)?;

    // We can't perform a recover tickets operation if we still have tickets available
    if !available_tickets.is_empty() {
        return Err(ContractError::StillHaveAvailableTickets {});
    }

    // Flag to avoid recovering multiple times at the same time
    let pending_ticket_update = PENDING_TICKET_UPDATE.load(deps.storage)?;
    if pending_ticket_update {
        return Err(ContractError::PendingTicketUpdate {});
    }
    PENDING_TICKET_UPDATE.save(deps.storage, &true)?;

    let used_tickets = USED_TICKETS_COUNTER.load(deps.storage)?;

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
        timestamp,
        None,
        Some(account_sequence),
        OperationType::AllocateTickets {
            number: number_to_allocate,
        },
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RecoverTickets.as_str())
        .add_attribute("sender", sender)
        .add_attribute("account_sequence", account_sequence.to_string()))
}

fn recover_xrpl_token_registration(
    deps: DepsMut,
    timestamp: u64,
    sender: Addr,
    issuer: String,
    currency: String,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::RecoverXRPLTokenRegistration,
    )?;

    let key = build_xrpl_token_key(&issuer, &currency);

    // The token must be registered for it to be recovered
    let mut token = XRPL_TOKENS
        .load(deps.storage, key.clone())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    // Check that the token is in inactive state, which means the trust set operation failed and we can recover it
    if token.state.ne(&TokenState::Inactive) {
        return Err(ContractError::XRPLTokenNotInactive {});
    }

    // Put the state back to Processing since we are going to try to activate it again
    token.state = TokenState::Processing;
    XRPL_TOKENS.save(deps.storage, key, &token)?;

    // Create the pending operation to approve the token again
    let config = CONFIG.load(deps.storage)?;
    let ticket = allocate_ticket(deps.storage)?;

    create_pending_operation(
        deps.storage,
        timestamp,
        Some(ticket),
        None,
        OperationType::TrustSet {
            issuer: issuer.clone(),
            currency: currency.clone(),
            trust_set_limit_amount: config.trust_set_limit_amount,
        },
    )?;

    Ok(Response::new()
        .add_attribute(
            "action",
            ContractActions::RecoverXRPLTokenRegistration.as_str(),
        )
        .add_attribute("sender", sender)
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency))
}

fn save_signature(
    deps: DepsMut,
    sender: Addr,
    operation_id: u64,
    operation_version: u64,
    signature: &str,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::SaveSignature,
    )?;

    add_signature(
        deps,
        operation_id,
        operation_version,
        sender.clone(),
        signature.to_string(),
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::SaveSignature.as_str())
        .add_attribute("sender", sender)
        .add_attribute("operation_id", operation_id.to_string())
        .add_attribute("signature", signature))
}

fn send_to_xrpl(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    recipient: String,
    deliver_amount: Option<Uint128>,
) -> CoreumResult<ContractError> {
    assert_bridge_active(deps.as_ref())?;
    // Check that we are only sending 1 type of coin
    let funds = one_coin(&info)?;

    // Check that the recipient is a valid XRPL address
    validate_xrpl_address(&recipient)?;

    // We don't allow sending to a prohibited addresses
    check_address_is_prohibited(deps.storage, recipient.clone())?;

    // We check that deliver_amount is not greater than the funds sent
    if deliver_amount.is_some() && deliver_amount.unwrap().gt(&funds.amount) {
        return Err(ContractError::InvalidDeliverAmount {});
    }

    let decimals;
    let mut amount_to_send;
    let max_amount;
    let remainder;
    let issuer;
    let currency;
    // We check if the token we are sending is an XRPL originated token or not
    if let Some(xrpl_token) = XRPL_TOKENS
        .idx
        .coreum_denom
        .item(deps.storage, funds.denom.clone())
        .map(|res| res.map(|pk_token| pk_token.1))?
    {
        // If it's an XRPL originated token we need to check that it's enabled and if it is apply the sending precision
        if xrpl_token.state.ne(&TokenState::Enabled) {
            return Err(ContractError::TokenNotEnabled {});
        }

        issuer = xrpl_token.issuer;
        currency = xrpl_token.currency;
        if is_token_xrp(&issuer, &currency) {
            // The deliver amount field cannot be sent for XRP, it's reserved for XRPL tokens that can have transfer rate
            if deliver_amount.is_some() {
                return Err(ContractError::DeliverAmountIsProhibited {});
            }
            decimals = XRP_DECIMALS;
        } else {
            decimals = XRPL_TOKENS_DECIMALS;
        }

        // We calculate the amount after applying the bridging fees for that token
        let amount_after_bridge_fees =
            amount_after_bridge_fees(funds.amount, xrpl_token.bridging_fee)?;

        // We don't need any decimal conversion because the token is an XRPL originated token and they are issued with same decimals
        (amount_to_send, remainder) = truncate_amount(
            xrpl_token.sending_precision,
            decimals,
            amount_after_bridge_fees,
        )?;

        // If deliver_amount was sent, we must check that it's less or equal than amount_to_send after bridge fees (without truncating) are applied
        if deliver_amount.is_some() {
            if deliver_amount.unwrap().gt(&amount_after_bridge_fees) {
                return Err(ContractError::InvalidDeliverAmount {});
            }
            let (truncated_amount, _) = truncate_amount(
                xrpl_token.sending_precision,
                decimals,
                deliver_amount.unwrap(),
            )?;

            max_amount = Some(amount_to_send);
            amount_to_send = truncated_amount;
        } else {
            // If token is XRP, we set the max amount to None because this token cannot have max_amount
            if is_token_xrp(&issuer, &currency) {
                max_amount = None;
            } else {
                max_amount = Some(amount_to_send);
            }
        }

        handle_fee_collection(
            deps.storage,
            xrpl_token.bridging_fee,
            xrpl_token.coreum_denom,
            remainder,
        )?;
    } else {
        // If it's not an XRPL originated token we need to check that it's registered as a Coreum originated token and that it's enabled
        let coreum_token = COREUM_TOKENS
            .load(deps.storage, funds.denom.clone())
            .map_err(|_| ContractError::TokenNotRegistered {})?;
        if coreum_token.state.ne(&TokenState::Enabled) {
            return Err(ContractError::TokenNotEnabled {});
        }

        // This field is reserved for XRPL originated tokens (except XRP)
        if deliver_amount.is_some() {
            return Err(ContractError::DeliverAmountIsProhibited {});
        }

        let config = CONFIG.load(deps.storage)?;

        decimals = coreum_token.decimals;
        issuer = config.bridge_xrpl_address;
        currency = coreum_token.xrpl_currency;

        // Since this is a Coreum originated token with different decimals, we are first going to truncate according to sending precision and then we will convert
        // to corresponding XRPL decimals
        let remainder;
        (amount_to_send, remainder) = truncate_and_convert_amount(
            coreum_token.sending_precision,
            decimals,
            XRPL_TOKENS_DECIMALS,
            funds.amount,
            coreum_token.bridging_fee,
        )?;

        handle_fee_collection(
            deps.storage,
            coreum_token.bridging_fee,
            coreum_token.denom.clone(),
            remainder,
        )?;

        // For Coreum originated tokens we need to check that we are not going over the amount
        // that the bridge will hold in escrow
        if deps
            .querier
            .query_balance(env.contract.address, coreum_token.denom)?
            .amount
            .gt(&coreum_token.max_holding_amount)
        {
            return Err(ContractError::MaximumBridgedAmountReached {});
        }

        // Coreum originated tokens never have transfer rate so the max amount will be the same as amount to send
        max_amount = Some(amount_to_send);
    }

    // We validate that both amount and max_amount on the operation contain valid XRPL amounts
    validate_xrpl_amount(amount_to_send)?;
    if max_amount.is_some() {
        validate_xrpl_amount(max_amount.unwrap())?;
    }

    // Get a ticket and store the pending operation
    let ticket = allocate_ticket(deps.storage)?;
    create_pending_operation(
        deps.storage,
        env.block.time.seconds(),
        Some(ticket),
        None,
        OperationType::CoreumToXRPLTransfer {
            issuer,
            currency,
            amount: amount_to_send,
            max_amount,
            sender: info.sender.clone(),
            recipient: recipient.clone(),
        },
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::SendToXRPL.as_str())
        .add_attribute("sender", info.sender)
        .add_attribute("recipient", recipient)
        .add_attribute("coin", funds.to_string()))
}

#[allow(clippy::too_many_arguments)]
fn update_xrpl_token(
    deps: DepsMut,
    sender: Addr,
    issuer: String,
    currency: String,
    state: Option<TokenState>,
    sending_precision: Option<i32>,
    bridging_fee: Option<Uint128>,
    max_holding_amount: Option<Uint128>,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::UpdateXRPLToken,
    )?;
    assert_bridge_active(deps.as_ref())?;

    let key = build_xrpl_token_key(&issuer, &currency);

    let mut token = XRPL_TOKENS
        .load(deps.storage, key.clone())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    set_token_state(&mut token.state, state)?;

    let decimals = if is_token_xrp(&issuer, &currency) {
        XRP_DECIMALS
    } else {
        XRPL_TOKENS_DECIMALS
    };
    set_token_sending_precision(&mut token.sending_precision, sending_precision, decimals)?;

    set_token_bridging_fee(&mut token.bridging_fee, bridging_fee)?;

    // Get the current bridged amount for this token to verify that we are not setting a max_holding_amount that is less than the current amount
    let current_bridged_amount = deps
        .querier
        .query_supply(token.coreum_denom.clone())?
        .amount;

    set_token_max_holding_amount(
        current_bridged_amount,
        &mut token.max_holding_amount,
        max_holding_amount,
    )?;

    XRPL_TOKENS.save(deps.storage, key, &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::UpdateXRPLToken.as_str())
        .add_attribute("sender", sender)
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency))
}

#[allow(clippy::too_many_arguments)]
fn update_coreum_token(
    deps: DepsMut,
    env: Env,
    sender: Addr,
    denom: String,
    state: Option<TokenState>,
    sending_precision: Option<i32>,
    bridging_fee: Option<Uint128>,
    max_holding_amount: Option<Uint128>,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::UpdateCoreumToken,
    )?;
    assert_bridge_active(deps.as_ref())?;

    let mut token = COREUM_TOKENS
        .load(deps.storage, denom.clone())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    set_token_state(&mut token.state, state)?;
    set_token_sending_precision(
        &mut token.sending_precision,
        sending_precision,
        token.decimals,
    )?;
    set_token_bridging_fee(&mut token.bridging_fee, bridging_fee)?;

    // Get the current bridged amount for this token to verify that we are not setting a max_holding_amount that is less than the current amount
    let current_bridged_amount = deps
        .querier
        .query_balance(env.contract.address, token.denom.clone())?
        .amount;
    set_token_max_holding_amount(
        current_bridged_amount,
        &mut token.max_holding_amount,
        max_holding_amount,
    )?;

    COREUM_TOKENS.save(deps.storage, denom.clone(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::UpdateCoreumToken.as_str())
        .add_attribute("sender", sender)
        .add_attribute("denom", denom))
}

fn update_xrpl_base_fee(
    deps: DepsMut,
    sender: Addr,
    xrpl_base_fee: u64,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::UpdateXRPLBaseFee,
    )?;

    // Update the value in config
    let mut config = CONFIG.load(deps.storage)?;
    config.xrpl_base_fee = xrpl_base_fee;
    CONFIG.save(deps.storage, &config)?;

    // Let's collect all operations in storage and update them
    let operations: Vec<(u64, Operation)> = PENDING_OPERATIONS
        .range(deps.storage, None, None, Order::Ascending)
        .filter_map(Result::ok)
        .collect();

    // For each operation in PENDING_OPERATIONS we increase the version by 1 and delete all signatures
    for operation in &operations {
        PENDING_OPERATIONS.save(
            deps.storage,
            operation.0,
            &Operation {
                id: operation.1.id.clone(),
                version: operation.1.version + 1,
                ticket_sequence: operation.1.ticket_sequence,
                account_sequence: operation.1.account_sequence,
                signatures: vec![],
                operation_type: operation.1.operation_type.clone(),
                xrpl_base_fee,
            },
        )?;
    }

    Ok(Response::new()
        .add_attribute("action", ContractActions::UpdateXRPLBaseFee.as_str())
        .add_attribute("sender", sender)
        .add_attribute("new_xrpl_base_fee", xrpl_base_fee.to_string()))
}

fn claim_relayer_fees(
    deps: DepsMut,
    sender: Addr,
    amounts: Vec<Coin>,
) -> CoreumResult<ContractError> {
    assert_bridge_active(deps.as_ref())?;

    // If fees were never collected for this address we don't allow the claim
    if FEES_COLLECTED
        .may_load(deps.storage, sender.clone())?
        .is_none()
    {
        return Err(ContractError::UnauthorizedSender {});
    };

    substract_relayer_fees(deps.storage, &sender, &amounts)?;

    let send_msg = BankMsg::Send {
        to_address: sender.to_string(),
        amount: amounts,
    };

    Ok(Response::new()
        .add_attribute("action", ContractActions::ClaimFees.as_str())
        .add_attribute("sender", sender)
        .add_message(send_msg))
}

fn claim_pending_refund(
    deps: DepsMut,
    sender: Addr,
    pending_refund_id: String,
) -> CoreumResult<ContractError> {
    assert_bridge_active(deps.as_ref())?;
    let coin = remove_pending_refund(deps.storage, &sender, pending_refund_id)?;

    let send_msg = BankMsg::Send {
        to_address: sender.to_string(),
        amount: vec![coin],
    };

    Ok(Response::new()
        .add_attribute("action", ContractActions::ClaimRefunds.as_str())
        .add_attribute("sender", sender)
        .add_message(send_msg))
}

fn halt_bridge(deps: DepsMut, sender: Addr) -> CoreumResult<ContractError> {
    check_authorization(deps.as_ref().storage, &sender, &ContractActions::HaltBridge)?;
    // No point halting a bridge that is already halted
    assert_bridge_active(deps.as_ref())?;
    update_bridge_state(deps.storage, BridgeState::Halted)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::HaltBridge.as_str())
        .add_attribute("sender", sender))
}

fn resume_bridge(deps: DepsMut, sender: Addr) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::ResumeBridge,
    )?;

    // Can't resume the bridge if there is a pending rotate keys ongoing
    if PENDING_ROTATE_KEYS.load(deps.storage)? {
        return Err(ContractError::RotateKeysOngoing {});
    }

    update_bridge_state(deps.storage, BridgeState::Active)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::ResumeBridge.as_str())
        .add_attribute("sender", sender))
}

fn rotate_keys(
    deps: DepsMut,
    env: Env,
    sender: Addr,
    new_relayers: Vec<Relayer>,
    new_evidence_threshold: u32,
) -> CoreumResult<ContractError> {
    check_authorization(deps.as_ref().storage, &sender, &ContractActions::RotateKeys)?;

    // If there is already a pending rotate keys ongoing, we don't allow another one until that one is confirmed
    if PENDING_ROTATE_KEYS.load(deps.storage)? {
        return Err(ContractError::RotateKeysOngoing {});
    }
    // We set the pending rotate keys flag to true so that we don't allow another rotate keys operation until this one is confirmed
    PENDING_ROTATE_KEYS.save(deps.storage, &true)?;

    // We halt the bridge
    update_bridge_state(deps.storage, BridgeState::Halted)?;

    // Validate the new relayer set so that we are sure that the new set is valid (e.g. no duplicated relayers, etc.)
    validate_relayers(deps.as_ref(), &new_relayers, new_evidence_threshold)?;

    let ticket = allocate_ticket(deps.storage)?;

    create_pending_operation(
        deps.storage,
        env.block.time.seconds(),
        Some(ticket),
        None,
        OperationType::RotateKeys {
            new_relayers,
            new_evidence_threshold,
        },
    )?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RotateKeys.as_str())
        .add_attribute("sender", sender))
}

fn update_prohibited_xrpl_addresses(
    deps: DepsMut,
    sender: Addr,
    prohibited_xrpl_addresses: Vec<String>,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::UpdateProhibitedXRPLAddresses,
    )?;

    // We clear the previous prohibited addresses
    PROHIBITED_XRPL_ADDRESSES.clear(deps.storage);

    // We add the current multisig address which is always prohibited
    let config = CONFIG.load(deps.storage)?;
    PROHIBITED_XRPL_ADDRESSES.save(deps.storage, config.bridge_xrpl_address, &Empty {})?;

    // Add all prohibited addresses provided
    for prohibited_xrpl_address in prohibited_xrpl_addresses {
        // Validate the address that we are adding, to not add useless things
        validate_xrpl_address(&prohibited_xrpl_address)?;
        PROHIBITED_XRPL_ADDRESSES.save(deps.storage, prohibited_xrpl_address, &Empty {})?;
    }

    Ok(Response::new()
        .add_attribute(
            "action",
            ContractActions::UpdateProhibitedXRPLAddresses.as_str(),
        )
        .add_attribute("sender", sender))
}

fn cancel_pending_operation(
    deps: DepsMut,
    sender: Addr,
    operation_id: u64,
) -> CoreumResult<ContractError> {
    check_authorization(
        deps.as_ref().storage,
        &sender,
        &ContractActions::CancelPendingOperation,
    )?;

    let operation = check_operation_exists(deps.storage, operation_id)?;
    // We'll provide a TransactionResult::Invalid evidence to the handlers so that they perform the right action
    let transaction_result = &TransactionResult::Invalid;
    let operation_result = match operation.operation_type {
        OperationType::AllocateTickets { .. } => Some(TicketsAllocation { tickets: None }),
        _ => None,
    };
    let mut response = Response::new();

    // We handle the operation with an invalid result
    handle_operation(
        deps.storage,
        &operation,
        &operation_result,
        transaction_result,
        &None,
        operation_id,
        operation.ticket_sequence,
        &mut response,
    )?;

    Ok(response
        .add_attribute("action", ContractActions::CancelPendingOperation.as_str())
        .add_attribute("sender", sender))
}

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => to_json_binary(&query_config(deps)?),
        QueryMsg::XRPLTokens {
            start_after_key,
            limit,
        } => to_json_binary(&query_xrpl_tokens(deps, start_after_key, limit)),
        QueryMsg::CoreumTokens {
            start_after_key,
            limit,
        } => to_json_binary(&query_coreum_tokens(deps, start_after_key, limit)),
        QueryMsg::Ownership {} => to_json_binary(&get_ownership(deps.storage)?),
        QueryMsg::PendingOperations {
            start_after_key,
            limit,
        } => to_json_binary(&query_pending_operations(deps, start_after_key, limit)),
        QueryMsg::AvailableTickets {} => to_json_binary(&query_available_tickets(deps)?),
        QueryMsg::PendingRefunds {
            address,
            start_after_key,
            limit,
        } => to_json_binary(&query_pending_refunds(
            deps,
            address,
            start_after_key,
            limit,
        )),
        QueryMsg::FeesCollected { relayer_address } => {
            to_json_binary(&query_fees_collected(deps, relayer_address)?)
        }
        QueryMsg::BridgeState {} => to_json_binary(&query_bridge_state(deps)?),
        QueryMsg::TransactionEvidence { hash } => {
            to_json_binary(&query_transaction_evidence(deps, hash)?)
        }
        QueryMsg::TransactionEvidences {
            start_after_key,
            limit,
        } => to_json_binary(&query_transaction_evidences(deps, start_after_key, limit)),
        QueryMsg::ProcessedTx { hash } => to_json_binary(&query_processed_tx(deps, hash)),
        QueryMsg::ProcessedTxs {
            start_after_key,
            limit,
        } => to_json_binary(&query_processed_txs(deps, start_after_key, limit)),
        QueryMsg::ProhibitedXRPLAddresses {} => {
            to_json_binary(&query_prohibited_xrpl_addresses(deps))
        }
    }
}

fn query_config(deps: Deps) -> StdResult<Config> {
    let config = CONFIG.load(deps.storage)?;
    Ok(config)
}

fn query_bridge_state(deps: Deps) -> StdResult<BridgeStateResponse> {
    let config = CONFIG.load(deps.storage)?;
    Ok(BridgeStateResponse {
        state: config.bridge_state,
    })
}

fn query_xrpl_tokens(
    deps: Deps,
    start_after_key: Option<String>,
    limit: Option<u32>,
) -> XRPLTokensResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;
    let tokens: Vec<XRPLToken> = XRPL_TOKENS
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(key, v)| {
            last_key = Some(key);
            v
        })
        .collect();

    XRPLTokensResponse { last_key, tokens }
}

fn query_coreum_tokens(
    deps: Deps,
    start_after_key: Option<String>,
    limit: Option<u32>,
) -> CoreumTokensResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;
    let tokens: Vec<CoreumToken> = COREUM_TOKENS
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(key, ct)| {
            last_key = Some(key);
            ct
        })
        .collect();

    CoreumTokensResponse { last_key, tokens }
}

fn query_pending_operations(
    deps: Deps,
    start_after_key: Option<u64>,
    limit: Option<u32>,
) -> PendingOperationsResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;
    let operations: Vec<Operation> = PENDING_OPERATIONS
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(key, v)| {
            last_key = Some(key);
            v
        })
        .collect();

    PendingOperationsResponse {
        last_key,
        operations,
    }
}

fn query_available_tickets(deps: Deps) -> StdResult<AvailableTicketsResponse> {
    let mut tickets = AVAILABLE_TICKETS.load(deps.storage)?;

    Ok(AvailableTicketsResponse {
        tickets: tickets.make_contiguous().to_vec(),
    })
}

fn query_fees_collected(deps: Deps, relayer_address: Addr) -> StdResult<FeesCollectedResponse> {
    let fees_collected = FEES_COLLECTED
        .may_load(deps.storage, relayer_address)?
        .unwrap_or_default();

    Ok(FeesCollectedResponse { fees_collected })
}

fn query_pending_refunds(
    deps: Deps,
    address: Addr,
    start_after_key: Option<(Addr, String)>,
    limit: Option<u32>,
) -> PendingRefundsResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;

    let pending_refunds: Vec<PendingRefund> = PENDING_REFUNDS
        .idx
        .address
        .prefix(address)
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(key, pr)| {
            last_key = Some(key);
            PendingRefund {
                id: pr.id,
                xrpl_tx_hash: pr.xrpl_tx_hash,
                coin: pr.coin,
            }
        })
        .collect();

    PendingRefundsResponse {
        last_key,
        pending_refunds,
    }
}

fn query_transaction_evidence(deps: Deps, hash: String) -> StdResult<TransactionEvidence> {
    let relayer_addresses = TX_EVIDENCES
        .may_load(deps.storage, hash.clone())?
        .map(|e| e.relayer_coreum_addresses);

    Ok(TransactionEvidence {
        hash,
        relayer_addresses: relayer_addresses.unwrap_or_default(),
    })
}

fn query_transaction_evidences(
    deps: Deps,
    start_after_key: Option<String>,
    limit: Option<u32>,
) -> TransactionEvidencesResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;
    let transaction_evidences: Vec<TransactionEvidence> = TX_EVIDENCES
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(evidence_hash, e)| {
            last_key = Some(evidence_hash.clone());
            TransactionEvidence {
                hash: evidence_hash,
                relayer_addresses: e.relayer_coreum_addresses,
            }
        })
        .collect();

    TransactionEvidencesResponse {
        last_key,
        transaction_evidences,
    }
}

fn query_processed_tx(deps: Deps, hash: String) -> bool {
    PROCESSED_TXS.has(deps.storage, hash)
}

fn query_processed_txs(
    deps: Deps,
    start_after_key: Option<String>,
    limit: Option<u32>,
) -> ProcessedTxsResponse {
    let limit = limit.unwrap_or(MAX_PAGE_LIMIT).min(MAX_PAGE_LIMIT);
    let start = start_after_key.map(Bound::exclusive);
    let mut last_key = None;
    let processed_txs: Vec<String> = PROCESSED_TXS
        .range(deps.storage, start, None, Order::Ascending)
        .take(limit as usize)
        .filter_map(Result::ok)
        .map(|(hash, _)| {
            last_key = Some(hash.clone());
            hash
        })
        .collect();

    ProcessedTxsResponse {
        last_key,
        processed_txs,
    }
}

fn query_prohibited_xrpl_addresses(deps: Deps) -> ProhibitedXRPLAddressesResponse {
    let prohibited_xrpl_addresses: Vec<String> = PROHIBITED_XRPL_ADDRESSES
        .range(deps.storage, None, None, Order::Ascending)
        .filter_map(Result::ok)
        .map(|(addr, _)| addr)
        .collect();

    ProhibitedXRPLAddressesResponse {
        prohibited_xrpl_addresses,
    }
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

pub fn validate_xrpl_currency(currency: &str) -> Result<(), ContractError> {
    // We check that currency is either a standard 3 character currency or it's a 40 character hex string currency, any other scenario is invalid
    match currency.len() {
        3 => {
            // XRP (uppercase) is not allowed
            if currency == "XRP" {
                return Err(ContractError::InvalidXRPLCurrency {});
            }
            // We check that all characters are uppercase/lowercase letters, numbers, or one of the allowed symbols: ?, !, @, #, $, %, ^, &, *, <, >, (, ), {, }, [, ], and |.
            if !currency
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || ALLOWED_CURRENCY_SYMBOLS.contains(&c))
            {
                return Err(ContractError::InvalidXRPLCurrency {});
            }
        }
        40 => {
            // The first 8 bits MUST not be 0x00
            if currency.starts_with("00") {
                return Err(ContractError::InvalidXRPLCurrency {});
            }
            // Must be uppercase hexadecimal
            if !currency
                .chars()
                .all(|c| c.is_ascii_hexdigit() && (c.is_numeric() || c.is_uppercase()))
            {
                return Err(ContractError::InvalidXRPLCurrency {});
            }
        }
        _ => return Err(ContractError::InvalidXRPLCurrency {}),
    }

    Ok(())
}

pub fn validate_coreum_token_decimals(decimals: u32) -> Result<(), ContractError> {
    if decimals > MAX_COREUM_TOKEN_DECIMALS {
        return Err(ContractError::InvalidDecimals {});
    }

    Ok(())
}

pub fn validate_sending_precision(
    sending_precision: i32,
    decimals: u32,
) -> Result<(), ContractError> {
    // Minimum and maximum sending precisions we allow
    if !(MIN_SENDING_PRECISION..=MAX_SENDING_PRECISION).contains(&sending_precision) {
        return Err(ContractError::InvalidSendingPrecision {});
    }

    if sending_precision > decimals as i32 {
        return Err(ContractError::InvalidSendingPrecision {});
    }
    Ok(())
}

// We are going to perform the same validation the CosmosSDK does for the denom
// which is the following Regex [a-zA-Z][a-zA-Z0-9/:._-]{2,127}
fn validate_coreum_denom(denom: &str) -> Result<(), ContractError> {
    if denom.len() < MIN_DENOM_LENGTH || denom.len() > MAX_DENOM_LENGTH {
        return Err(ContractError::InvalidDenom {});
    }

    // The first character must be a lowercase or uppercase letter
    if denom.starts_with(|c: char| !c.is_ascii_alphabetic()) {
        return Err(ContractError::InvalidDenom {});
    }

    // All the following characters must be alphanumeric or one of the following special characters: /:._-
    for c in denom.chars().skip(1) {
        if !c.is_ascii_alphanumeric() && !DENOM_SPECIAL_CHARACTERS.contains(&c) {
            return Err(ContractError::InvalidDenom {});
        }
    }

    Ok(())
}

// Function used to truncate the amount to not send tokens over the sending precision.
fn truncate_amount(
    sending_precision: i32,
    decimals: u32,
    amount: Uint128,
) -> Result<(Uint128, Uint128), ContractError> {
    // To get exactly by how much we need to divide the original amount
    // Example: if sending precision = -1. Exponent will be 15 - (-1) = 16 for XRPL tokens so we will divide the original amount by 1e16
    // Example: if sending precision = 14. Exponent will be 15 - 14 = 1 for XRPL tokens so we will divide the original amount by 10
    let exponent = decimals as i32 - sending_precision;

    let amount_to_send = amount.checked_div(Uint128::new(10u128.pow(exponent.unsigned_abs())))?;

    if amount_to_send.is_zero() {
        return Err(ContractError::AmountSentIsZeroAfterTruncation {});
    }

    let truncated_amount =
        amount_to_send.checked_mul(Uint128::new(10u128.pow(exponent.unsigned_abs())))?;
    let remainder = amount.checked_sub(truncated_amount)?;
    Ok((truncated_amount, remainder))
}

// Function used to convert the amount received from XRPL with XRPL decimals to the Coreum amount with Coreum decimals
pub fn convert_amount_decimals(
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

// Helper function to combine the conversion and truncation of amounts including substracting fees.
fn convert_and_truncate_amount(
    sending_precision: i32,
    from_decimals: u32,
    to_decimals: u32,
    amount: Uint128,
    bridging_fee: Uint128,
) -> Result<(Uint128, Uint128), ContractError> {
    let converted_amount = convert_amount_decimals(from_decimals, to_decimals, amount)?;

    let amount_after_fees = amount_after_bridge_fees(converted_amount, bridging_fee)?;

    // We save the remainder as well to add it to the fee collection
    let (truncated_amount, remainder) =
        truncate_amount(sending_precision, to_decimals, amount_after_fees)?;

    Ok((truncated_amount, remainder))
}

// Helper function to combine the truncation and conversion of amounts after substracting fees.
fn truncate_and_convert_amount(
    sending_precision: i32,
    from_decimals: u32,
    to_decimals: u32,
    amount: Uint128,
    bridging_fee: Uint128,
) -> Result<(Uint128, Uint128), ContractError> {
    // We calculate fees first and truncate afterwards because of XRPL not supporting values like 1e17 + 1
    let amount_after_fees = amount_after_bridge_fees(amount, bridging_fee)?;

    // We save the remainder as well to add it to fee collection
    let (truncated_amount, remainder) =
        truncate_amount(sending_precision, from_decimals, amount_after_fees)?;

    let converted_amount = convert_amount_decimals(from_decimals, to_decimals, truncated_amount)?;
    Ok((converted_amount, remainder))
}

// Helper function to validate that we are not sending an invalid amount to XRPL
// A valid amount is one that doesn't have more than 16 digits after trimming trailing zeroes
// Example: 1000000000000000000000000000 is valid
// Example: 1000000000000000000000000001 is not valid
fn validate_xrpl_amount(amount: Uint128) -> Result<(), ContractError> {
    let amount_str = amount.to_string();
    // Trim all zeroes at the end
    let amount_trimmed = amount_str.trim_end_matches('0');

    if amount_trimmed.len() > XRPL_MAX_TRUNCATED_AMOUNT_LENGTH {
        return Err(ContractError::InvalidXRPLAmount {});
    };

    Ok(())
}

fn convert_currency_to_xrpl_hexadecimal(currency: String) -> String {
    // Fill with zeros to get the correct hex representation in XRPL of our currency.
    format!("{:0<40}", hex::encode(currency)).to_uppercase()
}

// Helper function to check that the sender is authorized for an operation
fn check_authorization(
    storage: &dyn Storage,
    sender: &Addr,
    action: &ContractActions,
) -> Result<(), ContractError> {
    let mut user_types = vec![];
    if is_owner(storage, sender)? {
        user_types.push(UserType::Owner);
    }

    if is_relayer(storage, sender)? {
        user_types.push(UserType::Relayer);
    }

    if !user_types
        .iter()
        .any(|user_type| user_type.is_authorized(action))
    {
        return Err(ContractError::UnauthorizedSender {});
    }

    Ok(())
}

// Helper function to check that bridge is active
pub fn assert_bridge_active(deps: Deps) -> Result<(), ContractError> {
    let config = CONFIG.load(deps.storage)?;
    if config.bridge_state.ne(&BridgeState::Active) {
        return Err(ContractError::BridgeHalted {});
    }
    Ok(())
}

fn update_bridge_state(
    storage: &mut dyn Storage,
    bridge_state: BridgeState,
) -> Result<(), ContractError> {
    let mut config = CONFIG.load(storage)?;
    config.bridge_state = bridge_state;
    CONFIG.save(storage, &config)?;
    Ok(())
}

fn check_address_is_prohibited(
    storage: &dyn Storage,
    address: String,
) -> Result<(), ContractError> {
    if PROHIBITED_XRPL_ADDRESSES.has(storage, address) {
        return Err(ContractError::ProhibitedAddress {});
    }
    Ok(())
}
