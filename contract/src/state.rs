use std::collections::VecDeque;

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Coin, Empty, Uint128};
use cw_storage_plus::{Index, IndexList, IndexedMap, Item, Map, UniqueIndex};

use crate::{evidence::Evidences, operation::Operation, relayer::Relayer};

/// Top level storage key. Values must not conflict.
/// Each key is only one byte long to ensure we use the smallest possible storage keys.
#[repr(u8)]
pub enum TopKey {
    Config = b'1',
    TxEvidences = b'2',
    ProcessedTxs = b'3',
    CoreumTokens = b'4',
    XRPLTokens = b'5',
    UsedXRPLCurrenciesForCoreumTokens = b'6',
    AvailableTickets = b'7',
    UsedTickets = b'8',
    PendingOperations = b'9',
    PendingTicketUpdate = b'a',
    FeesCollected = b'b',
}

impl TopKey {
    const fn as_str(&self) -> &str {
        let array_ref = unsafe { std::mem::transmute::<_, &[u8; 1]>(self) };
        match core::str::from_utf8(array_ref) {
            Ok(a) => a,
            Err(_) => panic!("Non-utf8 enum value found. Use a-z, A-Z and 0-9"),
        }
    }
}

#[cw_serde]
pub struct Config {
    pub relayers: Vec<Relayer>,
    pub evidence_threshold: u32,
    pub used_ticket_sequence_threshold: u32,
    pub trust_set_limit_amount: Uint128,
    pub bridge_xrpl_address: String,
}

#[cw_serde]
pub struct XRPLToken {
    pub issuer: String,
    pub currency: String,
    pub coreum_denom: String,
    pub sending_precision: i32,
    pub max_holding_amount: Uint128,
    pub state: TokenState,
    pub bridging_fee: Uint128,
    pub transfer_rate: Option<Uint128>,
}

#[cw_serde]
pub enum TokenState {
    // Enabled tokens are tokens that can be bridged
    Enabled,
    // Disabled tokens are tokens that can be bridged but have been disabled by the admin and they must be activated again to be bridged
    Disabled,
    // Processing are tokens that have a TrustSet operation pending to be completed. If this operation succeeds they will be Enabled, if it fails they will be Inactive
    Processing,
    // Inactive tokens are tokens that can't be bridged because the trust set registration failed so it must be triggered again.
    Inactive,
}

#[cw_serde]
pub struct CoreumToken {
    pub denom: String,
    pub decimals: u32,
    pub xrpl_currency: String,
    pub sending_precision: i32,
    pub max_holding_amount: Uint128,
    pub state: TokenState,
    pub bridging_fee: Uint128,
}

pub const CONFIG: Item<Config> = Item::new(TopKey::Config.as_str());
// Tokens registered from XRPL side. These tokens are XRPL originated tokens - primary key is issuer+currency on XRPL
// XRPLTokens will have coreum_denom as a secondary index so that we can get the XRPLToken corresponding to a coreum_denom
pub struct XRPLTokensIndexes<'a> {
    pub coreum_denom: UniqueIndex<'a, String, XRPLToken, String>,
}

impl<'a> IndexList<XRPLToken> for XRPLTokensIndexes<'a> {
    fn get_indexes(&'_ self) -> Box<dyn Iterator<Item = &'_ dyn Index<XRPLToken>> + '_> {
        let v: Vec<&dyn Index<XRPLToken>> = vec![&self.coreum_denom];
        Box::new(v.into_iter())
    }
}

pub const XRPL_TOKENS: IndexedMap<String, XRPLToken, XRPLTokensIndexes> = IndexedMap::new(
    TopKey::XRPLTokens.as_str(),
    XRPLTokensIndexes {
        coreum_denom: UniqueIndex::new(
            |xrpl_token| xrpl_token.coreum_denom.clone(),
            "xrpl_token__coreum_denom",
        ),
    },
);
// Tokens registered from Coreum side. These tokens are coreum originated tokens that are registered to be bridged - key is denom on Coreum chain
// CoreumTokens will have xrpl_currency as a secondary index so that we can get the CoreumToken corresponding to a xrpl_currency
pub struct CoreumTokensIndexes<'a> {
    pub xrpl_currency: UniqueIndex<'a, String, CoreumToken, String>,
}

impl<'a> IndexList<CoreumToken> for CoreumTokensIndexes<'a> {
    fn get_indexes(&'_ self) -> Box<dyn Iterator<Item = &'_ dyn Index<CoreumToken>> + '_> {
        let v: Vec<&dyn Index<CoreumToken>> = vec![&self.xrpl_currency];
        Box::new(v.into_iter())
    }
}

pub const COREUM_TOKENS: IndexedMap<String, CoreumToken, CoreumTokensIndexes> = IndexedMap::new(
    TopKey::CoreumTokens.as_str(),
    CoreumTokensIndexes {
        xrpl_currency: UniqueIndex::new(
            |coreum_token| coreum_token.xrpl_currency.clone(),
            "coreum_token__xrpl_currency",
        ),
    },
);

// Evidences, when enough evidences are collected, the transaction hashes are stored in EXECUTED_EVIDENCE_OPERATIONS.
pub const TX_EVIDENCES: Map<String, Evidences> = Map::new(TopKey::TxEvidences.as_str());
// This will contain the transaction hashes of operations that have been executed (reached threshold) so that when the same hash is sent again they aren't executed again
pub const PROCESSED_TXS: Map<String, Empty> = Map::new(TopKey::ProcessedTxs.as_str());
// Current tickets available
pub const AVAILABLE_TICKETS: Item<VecDeque<u64>> = Item::new(TopKey::AvailableTickets.as_str());
// Counter we use to control the used tickets threshold.
// If we surpass this counter, we will trigger a new allocation operation.
// Every time we allocate new tickets (operation is accepted), we will substract the amount of new tickets allocated from this amount
pub const USED_TICKETS_COUNTER: Item<u32> = Item::new(TopKey::UsedTickets.as_str());
// Operations that are not accepted/rejected yet. When enough relayers send evidences confirming the correct execution or rejection of this operation,
// it will move to PROCESSED_TXS. Key is the ticket/sequence number
pub const PENDING_OPERATIONS: Map<u64, Operation> = Map::new(TopKey::PendingOperations.as_str());
// Flag to know if we are currently waiting for new_tickets to be allocated
pub const PENDING_TICKET_UPDATE: Item<bool> = Item::new(TopKey::PendingTicketUpdate.as_str());
// Fees collected that will be distributed to all relayers when they are claimed
pub const FEES_COLLECTED: Item<Vec<Coin>> = Item::new(TopKey::FeesCollected.as_str());

pub enum ContractActions {
    Instantiation,
    RegisterCoreumToken,
    RegisterXRPLToken,
    SendFromXRPLToCoreum,
    RecoverTickets,
    RecoverXRPLTokenRegistration,
    XRPLTransactionResult,
    SaveSignature,
    SendToXRPL,
    ClaimFees,
}

impl ContractActions {
    pub fn as_str(&self) -> &'static str {
        match self {
            ContractActions::Instantiation => "bridge_instantiation",
            ContractActions::RegisterCoreumToken => "register_coreum_token",
            ContractActions::RegisterXRPLToken => "register_xrpl_token",
            ContractActions::SendFromXRPLToCoreum => "send_from_xrpl_to_coreum",
            ContractActions::RecoverTickets => "recover_tickets",
            ContractActions::RecoverXRPLTokenRegistration => "recover_xrpl_token_registration",
            ContractActions::XRPLTransactionResult => "submit_xrpl_transaction_result",
            ContractActions::SaveSignature => "save_signature",
            ContractActions::SendToXRPL => "send_to_xrpl",
            ContractActions::ClaimFees => "claim_fees",
        }
    }
}
