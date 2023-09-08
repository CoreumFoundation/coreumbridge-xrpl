use std::vec;

use crate::{
    error::ContractError,
    msg::{
        CoreumTokenResponse, CoreumTokensResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
        XprlTokenResponse, XprlTokensResponse,
    },
    state::{Config, TokenCoreum, TokenXRP, CONFIG, TOKENS_COREUM, TOKENS_XRPL},
};
use coreum_wasm_sdk::{
    assetft::{Msg::Issue, BURNING, IBC, MINTING},
    core::{CoreumMsg, CoreumResult},
};
use cosmwasm_std::{
    entry_point, to_binary, Binary, CosmosMsg, Deps, DepsMut, Env, MessageInfo, Order, Response,
    StdResult, Uint128,
};
use cw2::set_contract_version;
use cw_ownable::{initialize_owner, Action};

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

const DEFAULT_MAX_LIMIT: u32 = 250;

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: InstantiateMsg,
) -> CoreumResult<ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    initialize_owner(
        deps.storage,
        deps.api,
        Some(deps.api.addr_validate(msg.admin.as_ref())?.as_ref()),
    )?;

    for address in msg.relayers.clone() {
        deps.api.addr_validate(address.as_ref())?;
    }

    //Threshold can't be more than number of relayers
    if msg.threshold > msg.relayers.len().try_into().unwrap() {
        return Err(ContractError::InvalidThreshold {});
    }

    let config = Config {
        relayers: msg.relayers,
        threshold: msg.threshold,
        min_tickets: msg.min_tickets,
    };

    CONFIG.save(deps.storage, &config)?;

    let xrp_issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
        symbol: "xrp".to_string(),
        subunit: "xrp".to_string(),
        precision: 15,
        initial_amount: Uint128::zero(),
        description: None,
        features: Some(vec![MINTING, BURNING, IBC]),
        burn_rate: Some("0.0".to_string()),
        send_commission_rate: Some("0.0".to_string()),
    }));

    let xrp_in_coreum = format!("{}-{}", "xrp", env.contract.address).to_lowercase();

    //We save the link between the denom in the Coreum chain and the denom in XPRL, so that when we receive
    //a token we can inform the relayers of what is being sent back.
    let token = TokenXRP {
        issuer: "xrp".to_string(),
        currency: "xrp".to_string(),
        coreum_denom: xrp_in_coreum,
    };

    TOKENS_XRPL.save(deps.storage, "xrp".to_string(), &token)?;

    Ok(Response::new()
        .add_attribute("action", "bridge_instantiation")
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

// ********** Queries **********
#[cfg_attr(not(feature = "library"), entry_point)]
pub fn query(deps: Deps, _env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::Config {} => to_binary(&query_config(deps)?),
        QueryMsg::XrplTokens { offset, limit } => {
            to_binary(&query_xprl_tokens(deps, offset, limit)?)
        }
        QueryMsg::CoreumTokens { offset, limit } => {
            to_binary(&query_coreum_tokens(deps, offset, limit)?)
        }
        QueryMsg::XrplToken { issuer, currency } => {
            to_binary(&query_xprl_token(deps, issuer, currency)?)
        }
        QueryMsg::CoreumToken { denom } => to_binary(&query_coreum_token(deps, denom)?),
    }
}

fn query_config(deps: Deps) -> StdResult<Config> {
    let config = CONFIG.load(deps.storage)?;
    Ok(config)
}

fn query_xprl_tokens(
    deps: Deps,
    offset: Option<u64>,
    limit: Option<u32>,
) -> StdResult<XprlTokensResponse> {
    let limit = limit.unwrap_or(DEFAULT_MAX_LIMIT).min(DEFAULT_MAX_LIMIT);
    let offset = offset.unwrap_or(0);
    let tokens: Vec<TokenXRP> = TOKENS_XRPL
        .range(deps.storage, None, None, Order::Ascending)
        .skip(offset as usize)
        .take(limit as usize)
        .filter_map(|v| v.ok())
        .map(|(_, v)| v)
        .collect();

    Ok(XprlTokensResponse { tokens })
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

fn query_xprl_token(deps: Deps, issuer: String, currency: String) -> StdResult<XprlTokenResponse> {
    let mut key = issuer;
    key.push_str(&currency);
    let token = TOKENS_XRPL.load(deps.storage, key)?;

    Ok(XprlTokenResponse { token })
}

fn query_coreum_token(deps: Deps, denom: String) -> StdResult<CoreumTokenResponse> {
    let token = TOKENS_COREUM.load(deps.storage, denom)?;

    Ok(CoreumTokenResponse { token })
}
