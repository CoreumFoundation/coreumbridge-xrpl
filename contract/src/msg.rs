use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::Addr;
use cw_ownable::{cw_ownable_execute, cw_ownable_query};

use crate::state::{Config, TokenCoreum, TokenXRP};

#[cw_serde]
pub struct InstantiateMsg {
    pub owner: Addr,
    //Addresses allowed to relay messages
    pub relayers: Vec<Addr>,
    //How many relayers need to provide evidence for a message
    pub evidence_threshold: u32,
    //If used tickets go over this amount we need to request new ones
    pub max_used_tickets: u32,
}

#[cw_ownable_execute]
#[cw_serde]
pub enum ExecuteMsg {
    RegisterCoreumToken { denom: String, decimals: u32 },
}

#[cw_ownable_query]
#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(Config)]
    Config {},
    #[returns(XrplTokensResponse)]
    XrplTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(CoreumTokensResponse)]
    CoreumTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(XrplTokenResponse)]
    XrplToken { issuer: String, currency: String },
    #[returns(CoreumTokenResponse)]
    CoreumToken { denom: String },
}

#[cw_serde]
pub struct XrplTokensResponse {
    pub tokens: Vec<TokenXRP>,
}

#[cw_serde]
pub struct CoreumTokensResponse {
    pub tokens: Vec<TokenCoreum>,
}

#[cw_serde]
pub struct XrplTokenResponse {
    pub token: TokenXRP,
}

#[cw_serde]
pub struct CoreumTokenResponse {
    pub token: TokenCoreum,
}
