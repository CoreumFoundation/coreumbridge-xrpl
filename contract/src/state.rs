use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty};
use cw_storage_plus::{Item, Map};

/// Top level storage key. Values must not conflict.
/// Each key is only one byte long to ensure we use the smallest possible storage keys.
#[repr(u8)]
pub enum TopKey {
    Config = b'1',
    Evidences = b'2',
    ExecutedEvidenceOperations = b'3',
    CoreumTokens = b'4',
    XRPLTokens = b'5',
    XRPLCurrencies = b'6',
    CoreumDenoms = b'7',
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
}

#[cw_serde]
pub struct XRPLToken {
    pub issuer: Option<String>,
    pub currency: Option<String>,
    pub coreum_denom: String,
}

#[cw_serde]
pub struct CoreumToken {
    pub denom: String,
    pub decimals: u32,
    pub xrpl_currency: String,
}

pub const CONFIG: Item<Config> = Item::new(TopKey::Config.as_str());

// 1. For me personally these names are really confusing: COREUM_TOKENS, XRPL_TOKENS, XRPL_CURRENCIES, COREUM_DENOMS.
// Are they really clear for you ? Lets discuss.
// My idea is to use: smth like XRPL_TOKENS_ON_COREUM etc.

// 2. Also comments on top of XRPL_CURRENCIES and COREUM_DENOMS should be improved.

// 3. lets discuss where we use token, currency and denom.

//Tokens registered from Coreum side - key is denom on Coreum chain
pub const COREUM_TOKENS: Map<String, CoreumToken> = Map::new(TopKey::CoreumTokens.as_str());
//Tokens registered from XRPL side - key is issuer+currency on XRPL
pub const XRPL_TOKENS: Map<String, XRPLToken> = Map::new(TopKey::XRPLTokens.as_str());
// XRPL-Currencies used
pub const XRPL_CURRENCIES: Map<String, Empty> = Map::new(TopKey::XRPLCurrencies.as_str());
// Coreum denoms used
pub const COREUM_DENOMS: Map<String, Empty> = Map::new(TopKey::CoreumDenoms.as_str());
// Evidences, when enough evidences are collected, the transaction hashes are stored in EXECUTED_EVIDENCE_OPERATIONS.
pub const EVIDENCES: Map<String, Evidences> = Map::new(TopKey::Evidences.as_str()); // Is XRPL txid is key here ? Pls add comment
// This will contain the transaction hashes of operations that have been executed (reached threshold) so that when the same hash is sent again they aren't executed again
pub const EXECUTED_EVIDENCE_OPERATIONS: Map<String, Empty> = Map::new(TopKey::ExecutedEvidenceOperations.as_str());

pub enum ContractActions {
    Instantiation,
    RegisterCoreumToken,
    RegisterXRPLToken,
    SendFromXRPLToCoreum,
}

impl ContractActions {
    pub fn as_str(&self) -> &'static str {
        match self {
            ContractActions::Instantiation => "bridge_instantiation",
            ContractActions::RegisterCoreumToken => "register_coreum_token",
            ContractActions::RegisterXRPLToken => "register_xrpl_token",
            ContractActions::SendFromXRPLToCoreum => "send_from_xrpl_to_coreum",
        }
    }
}

#[cw_serde]
pub struct Evidences {
    pub relayers: Vec<Addr>,
}

pub fn build_xrpl_token_key(issuer: String, currency: String) -> String {
    let mut key = issuer.clone();
    key.push_str(currency.as_str());
    key
}
