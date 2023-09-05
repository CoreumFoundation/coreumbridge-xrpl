use crate::{
    error::ContractError,
    msg::{ExecuteMsg, InstantiateMsg, Side},
    state::{Config, CONFIG, COREUM_TO_XRPL, CURRENT_ID, TICKETS, TOKENS_COREUM, TOKENS_XRPL},
};
use coreum_wasm_sdk::{
    assetft::{Msg::Issue, BURNING, IBC, MINTING},
    core::{CoreumMsg, CoreumResult},
};
use cosmwasm_std::{entry_point, CosmosMsg, DepsMut, Env, MessageInfo, Response, Uint128, Uint512};
use cw2::set_contract_version;
use cw_ownable::{assert_owner, initialize_owner, Action};

// version info for migration info
const CONTRACT_NAME: &str = env!("CARGO_PKG_NAME");
const CONTRACT_VERSION: &str = env!("CARGO_PKG_VERSION");

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn instantiate(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: InstantiateMsg,
) -> CoreumResult<ContractError> {
    set_contract_version(deps.storage, CONTRACT_NAME, CONTRACT_VERSION)?;
    initialize_owner(deps.storage, deps.api, Some(info.sender.as_ref()))?;

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
    CURRENT_ID.save(deps.storage, &Uint512::zero())?;

    //We keep the tickets individually in a FIFO queue to make things easier later
    for ticket in msg.initial_tickets_from..=msg.initial_tickets_to {
        TICKETS.push_back(deps.storage, &ticket)?;
    }

    let mut messages = vec![];

    if let Some(xpr_tokens) = msg.initial_xrp_tokens {
        for token in xpr_tokens {
            //For each token provided in the xpr token array we create an equivalent token in Coreum with the information provided
            let issue_msg = CosmosMsg::from(CoreumMsg::AssetFT(Issue {
                symbol: token.clone().symbol.unwrap_or(token.clone().denom),
                subunit: token.clone().denom,
                precision: token.precision,
                initial_amount: Uint128::zero(),
                description: None,
                features: Some(vec![MINTING, BURNING, IBC]),
                burn_rate: Some("0.0".to_string()),
                send_commission_rate: Some("0.0".to_string()),
            }));

            let denom_coreum = format!("{}-{}", token.denom, env.contract.address).to_lowercase();

            //We check that it doesn't exist yet
            if TOKENS_XRPL.has(deps.storage, token.clone().denom) {
                return Err(ContractError::DuplicateToken {
                    side: "XRP".to_string(),
                });
            };
            TOKENS_XRPL.save(deps.storage, token.clone().denom, &token)?;

            //We save the link between the denom in the Coreum chain and the denom in XPRL, so that when we receive
            //a token we can inform the relayers of what is being sent back.
            COREUM_TO_XRPL.save(deps.storage, denom_coreum, &token.clone().denom)?;

            messages.push(issue_msg);
        }
    }

    if let Some(coreum_tokens) = msg.initial_coreum_tokens {
        for token in coreum_tokens {
            if TOKENS_COREUM.has(deps.storage, token.clone().denom) {
                return Err(ContractError::DuplicateToken {
                    side: "Coreum".to_string(),
                });
            };
            TOKENS_COREUM.save(deps.storage, token.clone().denom, &token)?;
        }
    }

    Ok(Response::new()
        .add_attribute("action", "bridge_instantiation")
        .add_attribute("contract_name", CONTRACT_NAME)
        .add_attribute("contract_version", CONTRACT_VERSION)
        .add_attribute("admin", info.sender)
        .add_messages(messages))
}

#[cfg_attr(not(feature = "library"), entry_point)]
pub fn execute(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    match msg {
        ExecuteMsg::EnableToken { denom, side } => enable_token(deps, info, denom, side),
        ExecuteMsg::DisableToken { denom, side } => disable_token(deps, info, denom, side),
        ExecuteMsg::UpdateOwnership(action) => update_ownership(deps, env, info, action),
    }
}

fn enable_token(
    deps: DepsMut,
    info: MessageInfo,
    denom: String,
    side: Side,
) -> Result<Response, ContractError> {
    assert_owner(deps.storage, &info.sender)?;

    match side {
        Side::Coreum => {
            let mut token = TOKENS_COREUM
                .load(deps.storage, denom.clone())
                .map_err(|_| ContractError::TokenDoesNotExist {})?;

            if token.enabled {
                return Err(ContractError::TokenAlreadyEnabled {});
            }

            token.enabled = true;
            TOKENS_COREUM.save(deps.storage, denom.clone(), &token)?;
        }
        Side::Xrpl => {
            let mut token = TOKENS_XRPL
                .load(deps.storage, denom.clone())
                .map_err(|_| ContractError::TokenDoesNotExist {})?;

            if token.enabled {
                return Err(ContractError::TokenAlreadyEnabled {});
            }

            token.enabled = true;
            TOKENS_XRPL.save(deps.storage, denom.clone(), &token)?;
        }
    }

    Ok(Response::new()
        .add_attribute("action", "enable_token")
        .add_attribute("denom", denom)
        .add_attribute("side", side.to_string()))
}

fn disable_token(
    deps: DepsMut,
    info: MessageInfo,
    denom: String,
    side: Side,
) -> Result<Response, ContractError> {
    assert_owner(deps.storage, &info.sender)?;

    match side {
        Side::Coreum => {
            let mut token = TOKENS_COREUM
                .load(deps.storage, denom.clone())
                .map_err(|_| ContractError::TokenDoesNotExist {})?;

            if !token.enabled {
                return Err(ContractError::TokenAlreadyDisabled {});
            }

            token.enabled = false;
            TOKENS_COREUM.save(deps.storage, denom.clone(), &token)?;
        }
        Side::Xrpl => {
            let mut token = TOKENS_XRPL
                .load(deps.storage, denom.clone())
                .map_err(|_| ContractError::TokenDoesNotExist {})?;

            if !token.enabled {
                return Err(ContractError::TokenAlreadyDisabled {});
            }

            token.enabled = false;
            TOKENS_XRPL.save(deps.storage, denom.clone(), &token)?;
        }
    }

    Ok(Response::new()
        .add_attribute("action", "disable_token")
        .add_attribute("denom", denom)
        .add_attribute("side", side.to_string()))
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
