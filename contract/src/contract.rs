use crate::{
    error::ContractError,
    evidence::{Evidence, hash_bytes, handle_evidence},
    msg::{
        CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
        XRPLTokenResponse, XRPLTokensResponse,
    },
    state::{
        Config, ContractActions, CoreumToken, XRPLToken, CONFIG,
        COREUM_DENOMS, COREUM_TOKENS, XRPL_CURRENCIES, XRPL_TOKENS,
    },
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
use sha2::{Digest, Sha256};

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

const DEFAULT_MAX_LIMIT: u32 = 250;
const XRP_SYMBOL: &str = "XRP";
const XRP_SUBUNIT: &str = "drop";

const COREUM_CURRENCY_PREFIX: &str = "coreum";
const XRPL_DENOM_PREFIX: &str = "xrpl";
const XRPL_TOKENS_DECIMALS: u32 = 15;

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

    for address in msg.relayers.clone() {
        deps.api.addr_validate(address.as_ref())?;
    }

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;

    //Threshold can't be more than number of relayers
    if msg.evidence_threshold > msg.relayers.len().try_into().unwrap() {
        return Err(ContractError::InvalidThreshold {});
    }

    let config = Config {
        relayers: msg.relayers,
        evidence_threshold: msg.evidence_threshold,
    };

    CONFIG.save(deps.storage, &config)?;

    let xrp_issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: XRP_SYMBOL.to_string(),
        subunit: XRP_SUBUNIT.to_string(),
        precision: 6,
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
        issuer: None,
        currency: None,
        coreum_denom: xrp_in_coreum,
    };

    XRPL_TOKENS.save(deps.storage, XRP_SYMBOL.to_string(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::Instantiation.as_str())
        .add_attribute("contract_name", CONTRACT_NAME)
        .add_attribute("contract_version", CONTRACT_VERSION)
        .add_attribute("admin", info.sender)
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
        ExecuteMsg::RegisterXRPLToken { issuer, currency } => {
            register_xrpl_token(deps, env, issuer, currency, info)
        }
        ExecuteMsg::AcceptEvidence { evidence } => {
            accept_evidence(deps.into_empty(), info.sender, evidence)
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
    denom: String,
    decimals: u32,
    sender: Addr,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &sender)?;

    if COREUM_TOKENS.has(deps.storage, denom.clone()) {
        return Err(ContractError::CoreumTokenAlreadyRegistered { denom });
    }

    // We generate a currency creating a Sha256 hash of the denom, the decimals and the current time
    let to_hash = format!("{}{}{}", denom, decimals, env.block.time.seconds()).into_bytes();
    let hex_string = hash_bytes(to_hash)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    let xrpl_currency = format!("{}{}", COREUM_CURRENCY_PREFIX, hex_string);

    if XRPL_CURRENCIES.has(deps.storage, xrpl_currency.clone()) {
        return Err(ContractError::RegistrationFailure {});
    }
    XRPL_CURRENCIES.save(deps.storage, xrpl_currency.clone(), &Empty {})?;

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
    info: MessageInfo,
) -> CoreumResult<ContractError> {
    assert_owner(deps.storage, &info.sender)?;

    // We want to check that exactly the issue fee was sent, not more.
    check_issue_fee(&deps, &info)?;

    let mut key = issuer.clone();
    key.push_str(currency.as_str());

    if XRPL_TOKENS.has(deps.storage, key.clone()) {
        return Err(ContractError::XRPLTokenAlreadyRegistered { issuer, currency });
    }

    // We generate a random denom creating a Sha256 hash of the issuer, currency, decimals and current time
    let mut hasher = Sha256::new();

    hasher.update(
        format!(
            "{}{}{}{}",
            issuer,
            currency.clone(),
            XRPL_TOKENS_DECIMALS,
            env.block.time.seconds()
        )
        .into_bytes(),
    );

    let output = hasher.finalize();

    // We encode the hash in hexadecimal and take the first 10 characters
    let hex_string = hex::encode(output)
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
    if COREUM_DENOMS.has(deps.storage, denom.clone()) {
        return Err(ContractError::RegistrationFailure {});
    };

    COREUM_DENOMS.save(deps.storage, denom.clone(), &Empty {})?;

    let token = XRPLToken {
        issuer: Some(issuer.clone()),
        currency: Some(currency.clone()),
        coreum_denom: denom.clone(),
    };

    XRPL_TOKENS.save(deps.storage, key, &token)?;

    Ok(Response::new()
        .add_message(issue_msg)
        .add_attribute("action", ContractActions::RegisterXRPLToken.as_str())
        .add_attribute("issuer", issuer)
        .add_attribute("currency", currency)
        .add_attribute("denom", denom))
}

fn accept_evidence(deps: DepsMut, sender: Addr, evidence: Evidence) -> CoreumResult<ContractError> {
    evidence.validate()?;
    let config = CONFIG.load(deps.storage)?;

    if !config.relayers.contains(&sender) {
        return Err(ContractError::UnauthorizedOperation {});
    }

    let denom;

    match evidence.clone() {
        Evidence::XRPLToCoreum {
            issuer, currency, ..
        } => {
            //Create issuer+currency key to find denom on coreum.
            let mut key = issuer.clone();
            key.push_str(currency.as_str());

            denom = XRPL_TOKENS
                .load(deps.storage, key)
                .map_err(|_| ContractError::TokenNotRegistered {})?;
        }
    }

    let mut response = Response::new();

    let threshold_reached = handle_evidence(deps, sender, evidence.get_hash(), evidence.get_tx_hash())?;

    if threshold_reached {
        match evidence {
            Evidence::XRPLToCoreum { tx_hash, issuer, currency, amount, recipient } => {
                response = add_mint_and_send(response, amount, denom.coreum_denom, recipient.clone());
                response = response
                .add_attribute("action", ContractActions::SendFromXRPLToCoreum.as_str())
                .add_attribute("hash", tx_hash)
                .add_attribute("issuer", issuer)
                .add_attribute("currency", currency)
                .add_attribute("amount", amount.to_string())
                .add_attribute("recipient", recipient.to_string())
                .add_attribute("threshold_reached", threshold_reached.to_string())
            },
        }
    }
        

    Ok(response)
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
        QueryMsg::XRPLToken { issuer, currency } => {
            to_binary(&query_xrpl_token(deps, issuer, currency)?)
        }
        QueryMsg::CoreumToken { denom } => to_binary(&query_coreum_token(deps, denom)?),
        QueryMsg::Ownership {} => to_binary(&get_ownership(deps.storage)?),
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
    let limit = limit.unwrap_or(DEFAULT_MAX_LIMIT).min(DEFAULT_MAX_LIMIT);
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
    let limit = limit.unwrap_or(DEFAULT_MAX_LIMIT).min(DEFAULT_MAX_LIMIT);
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

fn query_xrpl_token(
    deps: Deps,
    issuer: Option<String>,
    currency: Option<String>,
) -> StdResult<XRPLTokenResponse> {
    let mut key;
    if issuer.is_none() && currency.is_none() {
        key = XRP_SYMBOL.to_string();
    } else {
        key = issuer.unwrap();
        key.push_str(&currency.unwrap());
    }

    let token = XRPL_TOKENS.load(deps.storage, key)?;

    Ok(XRPLTokenResponse { token })
}

fn query_coreum_token(deps: Deps, denom: String) -> StdResult<CoreumTokenResponse> {
    let token = COREUM_TOKENS.load(deps.storage, denom)?;

    Ok(CoreumTokenResponse { token })
}

// ********** Helpers **********

fn check_issue_fee(deps: &DepsMut<CoreumQueries>, info: &MessageInfo) -> Result<(), ContractError> {
    let query_params_res: ParamsResponse = deps
        .querier
        .query(&CoreumQueries::AssetFT(Query::Params {}).into())?;

    if query_params_res.params.issue_fee != one_coin(info)? {
        return Err(ContractError::InvalidIssueFee {});
    }

    Ok(())
}

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
        amount: coins(amount.u128(), denom.clone()),
    });

    response.add_messages([mint_msg, send_msg])
}