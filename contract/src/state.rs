use std::collections::VecDeque;

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage};
use cw_storage_plus::{Item, Map};

use crate::error::ContractError;

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
    pub relayers: Vec<Addr>,
    pub evidence_threshold: u32,
    pub used_tickets_threshold: u32,
}

#[cw_serde]
pub struct XRPLToken {
    pub issuer: String,
    pub currency: String,
    pub coreum_denom: String,
}

#[cw_serde]
pub struct CoreumToken {
    pub denom: String,
    pub decimals: u32,
    pub xrpl_currency: String,
}

pub const CONFIG: Item<Config> = Item::new(TopKey::Config.as_str());
//Tokens registered from Coreum side. These tokens are native Coreum tokens that are registered to be bridged - key is denom on Coreum chain
pub const COREUM_TOKENS: Map<String, CoreumToken> = Map::new(TopKey::CoreumTokens.as_str());
//Tokens registered from XRPL side. These tokens are native XRPL tokens - key is issuer+currency on XRPL
pub const XRPL_TOKENS: Map<String, XRPLToken> = Map::new(TopKey::XRPLTokens.as_str());
// XRPL-Currencies used
pub const USED_XRPL_CURRENCIES_FOR_COREUM_TOKENS: Map<String, Empty> = Map::new(TopKey::UsedXRPLCurrenciesForCoreumTokens.as_str());
// Evidences, when enough evidences are collected, the transaction hashes are stored in EXECUTED_EVIDENCE_OPERATIONS.
pub const TX_EVIDENCES: Map<String, Evidences> = Map::new(TopKey::TxEvidences.as_str());
// This will contain the transaction hashes of operations that have been executed (reached threshold) so that when the same hash is sent again they aren't executed again
pub const PROCESSED_TXS: Map<String, Empty> =
    Map::new(TopKey::ProcessedTxs.as_str());
// Current tickets available
pub const AVAILABLE_TICKETS: Item<VecDeque<u64>> = Item::new(TopKey::AvailableTickets.as_str());
// Currently used tickets. Every time we allocate new tickets, we will substract the amount of new tickets allocated from this amount
pub const USED_TICKETS: Item<u32> = Item::new(TopKey::UsedTickets.as_str());
// Operations that are not confirmed/rejected yet. When enough relayers send evidences confirming the correct execution or rejection of this operation,
// it will move to PROCESSED_TXS. Key is the ticket/sequence number
pub const PENDING_OPERATIONS: Map<u64, Operation> = Map::new(TopKey::PendingOperations.as_str());
// Flag to know if we are currently waiting for new_tickets to be allocated
pub const PENDING_TICKET_UPDATE: Item<bool> = Item::new(TopKey::PendingTicketUpdate.as_str());

pub enum ContractActions {
    Instantiation,
    RegisterCoreumToken,
    RegisterXRPLToken,
    SendFromXRPLToCoreum,
    RecoverTickets,
    XRPLTransactionResult,
    RegisterSignature,
}

impl ContractActions {
    pub fn as_str(&self) -> &'static str {
        match self {
            ContractActions::Instantiation => "bridge_instantiation",
            ContractActions::RegisterCoreumToken => "register_coreum_token",
            ContractActions::RegisterXRPLToken => "register_xrpl_token",
            ContractActions::SendFromXRPLToCoreum => "send_from_xrpl_to_coreum",
            ContractActions::RecoverTickets => "recover_tickets",
            ContractActions::XRPLTransactionResult => "submit_xrpl_transaction_result",
            ContractActions::RegisterSignature => "register_signature",
        }
    }
}

#[cw_serde]
pub struct Evidences {
    pub relayers: Vec<Addr>,
}

#[cw_serde]
pub struct Operation {
    pub ticket_number: Option<u64>,
    pub sequence_number: Option<u64>,
    pub signatures: Vec<Signature>,
    pub operation_type: OperationType,
}

#[cw_serde]
pub enum OperationType {
    AllocateTickets { number: u32 },
}

#[cw_serde]
pub struct Signature {
    pub relayer: Addr,
    pub signature: String,
}

pub fn remove_pending_operation(
    storage: &mut dyn Storage,
    number: u64,
) -> Result<(), ContractError> {
    PENDING_OPERATIONS.remove(storage, number);
    PENDING_TICKET_UPDATE.save(storage, &false)?;

    Ok(())
}
