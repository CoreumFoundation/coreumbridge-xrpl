use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::{Addr, Coin, Uint128};
use cw_ownable::{cw_ownable_execute, cw_ownable_query};

#[allow(unused_imports)]
use crate::state::{Config, CoreumToken, XRPLToken};
use crate::{evidence::Evidence, operation::Operation, relayer::Relayer, state::TokenState};

#[cw_serde]
pub struct InstantiateMsg {
    pub owner: Addr,
    // Addresses allowed to relay messages
    pub relayers: Vec<Relayer>,
    // How many relayers need to provide evidence for a message
    pub evidence_threshold: u32,
    // Amount of tickets that  we can use before triggering a ticket allocation action
    pub used_ticket_sequence_threshold: u32,
    // Trust set limit amount that will be used when registering XRPL tokens
    pub trust_set_limit_amount: Uint128,
    // Address of multisig account on the XRPL
    pub bridge_xrpl_address: String,
}

#[cw_ownable_execute]
#[cw_serde]
pub enum ExecuteMsg {
    RegisterCoreumToken {
        denom: String,
        decimals: u32,
        sending_precision: i32,
        max_holding_amount: Uint128,
        bridging_fee: Uint128,
    },
    #[serde(rename = "register_xrpl_token")]
    RegisterXRPLToken {
        issuer: String,
        currency: String,
        sending_precision: i32,
        max_holding_amount: Uint128,
        bridging_fee: Uint128,
        // The Transfer Rate is an integer which represents the amount you must send for the recipient to get 1 billion units of the same token.
        // A Transfer Rate of 1005000000 is equivalent to a transfer fee of 0.5%. The value of Transfer Rate must be more than 1000000000 ("0%" fee) or
        // less or equal than 2000000000 (a "100%" fee). If it is not sent there will be no fee.
        transfer_rate: Option<Uint128>,
    },
    RecoverTickets {
        account_sequence: u64,
        number_of_tickets: Option<u32>,
    },
    #[serde(rename = "recover_xrpl_token_registration")]
    RecoverXRPLTokenRegistration {
        issuer: String,
        currency: String,
        // If the transfer rate needs to be modified because admin sent it wrong and registration failed, it can be done here.
        transfer_rate: Option<Uint128>,
    },
    SaveSignature {
        operation_id: u64,
        signature: String,
    },
    SaveEvidence {
        evidence: Evidence,
    },
    #[serde(rename = "send_to_xrpl")]
    SendToXRPL {
        recipient: String,
    },
    // All fields that can be updatable for XRPL originated tokens will be updated with this message
    // They are all optional, so any fields that have to be updated can be included in the message.
    #[serde(rename = "update_xrpl_token")]
    UpdateXRPLToken {
        issuer: String,
        currency: String,
        state: Option<TokenState>,
    },
    // All fields that can be updatable for Coreum tokens will be updated with this message.
    // They are all optional, so any fields that have to be updated can be included in the message.
    UpdateCoreumToken {
        denom: String,
        state: Option<TokenState>,
    },
    // Any relayer can claim fees at any point in time. They need to provide what they want to claim.
    ClaimFees {
        amounts: Vec<Coin>,
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
    #[returns(PendingOperationsResponse)]
    PendingOperations {},
    #[returns(AvailableTicketsResponse)]
    AvailableTickets {},
    #[returns(FeesCollectedResponse)]
    FeesCollected { relayer_address: Addr },
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
pub struct PendingOperationsResponse {
    pub operations: Vec<Operation>,
}

#[cw_serde]
pub struct AvailableTicketsResponse {
    pub tickets: Vec<u64>,
}

#[cw_serde]
pub struct FeesCollectedResponse {
    pub fees_collected: Vec<Coin>,
}
