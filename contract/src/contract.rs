use crate::{
    error::ContractError,
    msg::{
        CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
        XrplTokenResponse, XrplTokensResponse,
    },
    state::{
        Config, ContractActions, TokenCoreum, TokenXRPL, CONFIG, TOKENS_COREUM, TOKENS_XRPL,
        XRPL_CURRENCIES,
    },
};
use base64::{engine::general_purpose, Engine as _};
use coreum_wasm_sdk::{
    assetft::{Msg::Issue, ParamsResponse, Query, BURNING, IBC, MINTING},
    core::{CoreumMsg, CoreumQueries, CoreumResult},
};
use cosmwasm_std::{
    entry_point, to_binary, Addr, Binary, CosmosMsg, Deps, DepsMut, Empty, Env, MessageInfo, Order,
    Response, StdResult, Uint128,
};
use cw2::set_contract_version;
use cw_ownable::{assert_owner, get_ownership, initialize_owner, Action};
use cw_utils::one_coin;
use sha2::{Digest, Sha256};
use std::str;

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

const DEFAULT_MAX_LIMIT: u32 = 250;
const XRP_SYMBOL: &str = "XRP";
const XRP_SUBUNIT: &str = "drop";

const COREUM_CURRENCY_PREFIX: &str = "coreum";

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
    let query_params_res: ParamsResponse = deps
        .querier
        .query(&CoreumQueries::AssetFT(Query::Params {}).into())?;

    if query_params_res.params.issue_fee != one_coin(&info)? {
        return Err(ContractError::InvalidIssueFee {});
    }

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
    let token = TokenXRPL {
        issuer: None,
        currency: None,
        coreum_denom: xrp_in_coreum,
    };

    TOKENS_XRPL.save(deps.storage, XRP_SYMBOL.to_string(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::Instantiation.as_str())
        .add_attribute("contract_name", CONTRACT_NAME)
        .add_attribute("contract_version", CONTRACT_VERSION)
        .add_attribute("admin", info.sender)
        .add_message(xrp_issue_msg))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    match msg {
        ExecuteMsg::UpdateOwnership(action) => update_ownership(deps, env, info, action),
        ExecuteMsg::RegisterCoreumToken { denom, decimals } => {
            register_coreum_token(deps, env, denom, decimals, info.sender)
        }
    }
}

fn update_ownership(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    action: Action,
) -> Result<Response, ContractError> {
    let ownership = cw_ownable::update_ownership(deps, &env.block, &info.sender, action)?;
    Ok(Response::new().add_attributes(ownership.into_attributes()))
}

fn register_coreum_token(
    deps: DepsMut,
    env: Env,
    denom: String,
    decimals: u32,
    sender: Addr,
) -> Result<Response, ContractError> {
    assert_owner(deps.storage, &sender)?;

    if TOKENS_COREUM.has(deps.storage, denom.clone()) {
        return Err(ContractError::CoreumTokenAlreadyRegistered {
            denom: denom.clone(),
        });
    }

    // We generate a random currency creating a Sha256 hash of the denom, the decimals and the current time
    let mut hasher = Sha256::new();

    hasher
        .update(format!("{}{}{}", denom.clone(), decimals, env.block.time.seconds()).into_bytes());

    let output = hasher.finalize();

    // We encode the hash in base64 and take the first 10 characters
    let base64_string = general_purpose::STANDARD_NO_PAD
        .encode(output)
        .get(0..10)
        .unwrap()
        .to_string()
        .to_lowercase();

    let xrpl_currency = format!("{}{}", COREUM_CURRENCY_PREFIX, base64_string);

    if XRPL_CURRENCIES.has(deps.storage, xrpl_currency.clone()) {
        return Err(ContractError::RegistrationFailure {});
    }
    XRPL_CURRENCIES.save(deps.storage, xrpl_currency.clone(), &Empty {})?;

    let token = TokenCoreum {
        denom: denom.clone(),
        decimals,
        xrpl_currency: xrpl_currency.clone(),
    };
    TOKENS_COREUM.save(deps.storage, denom.clone(), &token)?;

    Ok(Response::new()
        .add_attribute("action", ContractActions::RegisterCoreumToken.as_str())
        .add_attribute("denom", denom)
        .add_attribute("decimals", decimals.to_string())
        .add_attribute("xrpl_currency_for_denom", xrpl_currency))
}

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => to_binary(&query_config(deps)?),
        QueryMsg::XrplTokens { offset, limit } => {
            to_binary(&query_xrpl_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumTokens { offset, limit } => {
            to_binary(&query_coreum_tokens(deps, offset, limit)?)
        }
        QueryMsg::XrplToken { issuer, currency } => {
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
) -> StdResult<XrplTokensResponse> {
    let limit = limit.unwrap_or(DEFAULT_MAX_LIMIT).min(DEFAULT_MAX_LIMIT);
    let offset = offset.unwrap_or(0);
    let tokens: Vec<TokenXRPL> = TOKENS_XRPL
        .range(deps.storage, None, None, Order::Ascending)
        .skip(offset as usize)
        .take(limit as usize)
        .filter_map(|v| v.ok())
        .map(|(_, v)| v)
        .collect();

    Ok(XrplTokensResponse { tokens })
}

fn query_coreum_tokens(
    deps: Deps,
    offset: Option<u64>,
    limit: Option<u32>,
) -> StdResult<CoreumTokensResponse> {
    let limit = limit.unwrap_or(DEFAULT_MAX_LIMIT).min(DEFAULT_MAX_LIMIT);
    let offset = offset.unwrap_or(0);
    let tokens: Vec<TokenCoreum> = TOKENS_COREUM
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
) -> StdResult<XrplTokenResponse> {
    let mut key;
    if issuer.is_none() && currency.is_none() {
        key = XRP_SYMBOL.to_string();
    } else {
        key = issuer.unwrap();
        key.push_str(&currency.unwrap());
    }

    let token = TOKENS_XRPL.load(deps.storage, key)?;

    Ok(XrplTokenResponse { token })
}

fn query_coreum_token(deps: Deps, denom: String) -> StdResult<CoreumTokenResponse> {
    let token = TOKENS_COREUM.load(deps.storage, denom)?;

    Ok(CoreumTokenResponse { token })
}
