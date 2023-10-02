use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::Addr;
use cw_ownable::{cw_ownable_execute, cw_ownable_query};

use crate::evidence::Evidence;
#[allow(unused_imports)]
use crate::state::{Config, CoreumToken, XRPLToken};

#[cw_serde]
pub struct InstantiateMsg {
    pub owner: Addr,
    //Addresses allowed to relay messages
    pub relayers: Vec<Addr>,
    //How many relayers need to provide evidence for a message
    pub evidence_threshold: u32,
}

#[cw_ownable_execute]
#[cw_serde]
pub enum ExecuteMsg {
    RegisterCoreumToken { denom: String, decimals: u32 },
    RegisterXRPLToken { issuer: String, currency: String },
    AcceptEvidence { evidence: Evidence },
}

#[cw_ownable_query]
#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(Config)]
    Config {},
    #[returns(XRPLTokensResponse)]
    XRPLTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(CoreumTokensResponse)]
    CoreumTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(XRPLTokenResponse)]
    XRPLToken {
        issuer: Option<String>,
        currency: Option<String>,
    },
    #[returns(CoreumTokenResponse)]
    CoreumToken { denom: String },
}

#[cw_serde]
pub struct XRPLTokensResponse {
    pub tokens: Vec<XRPLToken>,
}

#[cw_serde]
pub struct CoreumTokensResponse {
    pub tokens: Vec<CoreumToken>,
}

#[cw_serde]
pub struct XRPLTokenResponse {
    pub token: XRPLToken,
}

#[cw_serde]
pub struct CoreumTokenResponse {
    pub token: CoreumToken,
}
