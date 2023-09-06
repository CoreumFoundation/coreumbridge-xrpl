use std::collections::HashMap;

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Decimal, Uint512};
use cw_storage_plus::{Deque, Item, Map};

/// Top level storage key. Values must not conflict.
/// Each key is only one byte long to ensure we use the smallest possible storage keys.
#[repr(u8)]
pub enum TopKey {
    Config = b'c',
    Tickets = b't',
    Id = b'i',
    TokensCoreum = b'1',
    TokensXRPL = b'2',
    CoreumToXRPL = b'3',
    SigningQueue = b'S',
    OutgoingQueue = b'O',
    ConfirmedQueue = b'C',
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
    pub threshold: u32,
    pub min_tickets: u32,
}

#[cw_serde]
pub struct Token {
    pub symbol: Option<String>,
    pub denom: String,
    pub precision: u32,
    //How much commission does this token have
    pub commission: Decimal,
    //Fee charged by bridge (in subunits)
    //pub fee: u64,
    //If token is bridgeable or not
    pub enabled: bool,
}

#[cw_serde]
pub struct SigningTransaction {
    pub operation: Operation,
    pub evidences: HashMap<Addr, String>,
}

#[cw_serde]
pub struct Transaction {
    pub operation: Operation,
}

#[cw_serde]
pub enum Operation {}

pub const CONFIG: Item<Config> = Item::new(TopKey::Config.as_str());
pub const CURRENT_ID: Item<Uint512> = Item::new(TopKey::Id.as_str());
pub const TICKETS: Deque<u64> = Deque::new(TopKey::Tickets.as_str());
pub const TOKENS_COREUM: Map<String, Token> = Map::new(TopKey::TokensCoreum.as_str());
pub const TOKENS_XRPL: Map<String, Token> = Map::new(TopKey::TokensXRPL.as_str());
pub const COREUM_TO_XRPL: Map<String, String> = Map::new(TopKey::CoreumToXRPL.as_str());
pub const SIGNING_QUEUE: Map<Uint512, SigningTransaction> = Map::new(TopKey::SigningQueue.as_str());
pub const OUTGOING_QUEUE: Map<Uint512, Transaction> = Map::new(TopKey::OutgoingQueue.as_str());
pub const CONFIRMED_QUEUE: Map<Uint512, Transaction> = Map::new(TopKey::ConfirmedQueue.as_str());
