use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::{Addr, Coin, Uint128};
use cw_ownable::{cw_ownable_execute, cw_ownable_query};

#[allow(unused_imports)]
use crate::state::{Config, CoreumToken, XRPLToken};
use crate::{
    evidence::Evidence,
    operation::Operation,
    relayer::Relayer,
    state::{BridgeState, TokenState},
};

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
    },
    RecoverTickets {
        account_sequence: u64,
        number_of_tickets: Option<u32>,
    },
    #[serde(rename = "recover_xrpl_token_registration")]
    RecoverXRPLTokenRegistration {
        issuer: String,
        currency: String,
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
        // This optional field is only allowed for XRPL originated tokens and is used together with attached funds to work with XRPL transfer rate.
        // How it works:
        // 1. If the token is not XRPL originated or XRP, if this is sent, we will return an error
        // 2. If the token is XRPL originated, if this is not sent, amount = max_amount = funds sent - bridging_fee
        // 3. If the token is XRPL originated, if this is sent, amount = deliver_amount, max_amount = funds sent - bridging fee
        deliver_amount: Option<Uint128>,
    },
    // All fields that can be updatable for XRPL originated tokens will be updated with this message
    // They are all optional, so any fields that have to be updated can be included in the message.
    #[serde(rename = "update_xrpl_token")]
    UpdateXRPLToken {
        issuer: String,
        currency: String,
        state: Option<TokenState>,
        sending_precision: Option<i32>,
        bridging_fee: Option<Uint128>,
        max_holding_amount: Option<Uint128>,
    },
    // All fields that can be updatable for Coreum tokens will be updated with this message.
    // They are all optional, so any fields that have to be updated can be included in the message.
    UpdateCoreumToken {
        denom: String,
        state: Option<TokenState>,
        sending_precision: Option<i32>,
        bridging_fee: Option<Uint128>,
        max_holding_amount: Option<Uint128>,
    },
    // Claim refund. User who can claim amounts due to failed transactions can do it with this message.
    ClaimRefund {
        pending_refund_id: String,
    },
    // Any relayer can claim fees at any point in time. They need to provide what they want to claim.
    ClaimRelayerFees {
        amounts: Vec<Coin>,
    },
    // A relayer or the owner can halt the bridge operations if an issue is detected
    HaltBridge {},
    // Owner can resume the bridge that is in halted state
    ResumeBridge {},
    // Owner can trigger a rotate keys, removing and/or adding relayers
    RotateKeys {
        account_sequence: Option<u64>,
        new_relayers: Vec<Relayer>,
        new_evidence_threshold: u32,
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
    #[returns(PendingRefundsResponse)]
    PendingRefunds {
        address: Addr,
        offset: Option<u64>,
        limit: Option<u32>,
    },
    #[returns(BridgeStateResponse)]
    BridgeState {},
    #[returns(TransactionEvidences)]
    TransactionEvidences { hash: String },
    #[returns(TransactionsEvidencesResponse)]
    TransactionsEvidences {
        offset: Option<u64>,
        limit: Option<u32>,
    },
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

#[cw_serde]
pub struct PendingRefundsResponse {
    pub pending_refunds: Vec<PendingRefund>,
}

#[cw_serde]
pub struct PendingRefund {
    pub id: String,
    pub coin: Coin,
}

#[cw_serde]
pub struct BridgeStateResponse {
    pub state: BridgeState,
}

#[cw_serde]
pub struct TransactionEvidences {
    pub hash: String,
    pub relayer_addresses: Vec<Addr>,
}

#[cw_serde]
pub struct TransactionsEvidencesResponse {
    pub transactions_evidences: Vec<TransactionEvidences>,
}
