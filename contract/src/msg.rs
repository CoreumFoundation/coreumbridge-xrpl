use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::Addr;
use cw_ownable::{cw_ownable_execute, cw_ownable_query};

#[allow(unused_imports)]
use crate::state::{Config, CoreumToken, XRPLToken};
use crate::{evidence::Evidence, state::Operation};

#[cw_serde]
pub struct InstantiateMsg {
    pub owner: Addr,
    //Addresses allowed to relay messages
    pub relayers: Vec<Addr>,
    //How many relayers need to provide evidence for a message
    pub evidence_threshold: u32,
    //Amount of tickets that  we can use before triggering a ticket allocation action
    pub used_tickets_threshold: u32,
}

#[cw_ownable_execute]
#[cw_serde]
pub enum ExecuteMsg {
    RegisterCoreumToken {
        denom: String,
        decimals: u32,
    },
    #[serde(rename = "register_xrpl_token")]
    RegisterXRPLToken {
        issuer: String,
        currency: String,
    },
    RecoverTickets {
        sequence_number: u64,
        number_of_tickets: Option<u32>,
    },
    RegisterSignature {
        operation_id: u64,
        signature: String,
    },
    SendEvidence {
        evidence: Evidence,
    },
}

#[cw_ownable_query]
#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(Config)]
    Config {},
    #[returns(XRPLTokensResponse)]
    #[serde(rename = "xrpl_tokens")]
    XRPLTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(CoreumTokensResponse)]
    CoreumTokens {
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(CoreumTokenResponse)]
    CoreumToken { denom: String },
    #[returns(PendingOperationsResponse)]
    PendingOperations {},
    #[returns(AvailableTicketsResponse)]
    AvailableTickets {},
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
pub struct CoreumTokenResponse {
    pub token: CoreumToken,
}

#[cw_serde]
pub struct PendingOperationsResponse {
    pub operations: Vec<Operation>,
}

#[cw_serde]
pub struct AvailableTicketsResponse {
    pub tickets: Vec<u64>,
}
