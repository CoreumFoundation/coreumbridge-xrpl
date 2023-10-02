use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty};
use cw_storage_plus::{Item, Map};

/// Top level storage key. Values must not conflict.
/// Each key is only one byte long to ensure we use the smallest possible storage keys.
#[repr(u8)]
pub enum TopKey {
    Config = b'c',
    Evidences = b'E',
    ExecutedOperations = b'O',
    CoreumTokens = b'1',
    XRPLTokens = b'2',
    XRPLCurrencies = b'3',
    CoreumDenoms = b'4',
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
//Tokens registered from Coreum side - key is denom on Coreum chain
pub const COREUM_TOKENS: Map<String, CoreumToken> = Map::new(TopKey::CoreumTokens.as_str());
//Tokens registered from XRPL side - key is issuer+currency on XRPL
pub const XRPL_TOKENS: Map<String, XRPLToken> = Map::new(TopKey::XRPLTokens.as_str());
// XRPL-Currencies used
pub const XRPL_CURRENCIES: Map<String, Empty> = Map::new(TopKey::XRPLCurrencies.as_str());
// Coreum denoms used
pub const COREUM_DENOMS: Map<String, Empty> = Map::new(TopKey::CoreumDenoms.as_str());
// Evidences, when enough evidences are collected, hashes are moved to completed operations
pub const EVIDENCES: Map<String, Evidences> = Map::new(TopKey::Evidences.as_str());
// Completed operations so that we don't process the same operation twice
pub const EXECUTED_OPERATIONS: Map<String, String> = Map::new(TopKey::ExecutedOperations.as_str());

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

pub enum Operation {
    XRPLToCoreum,
}

impl Operation {
    pub fn as_str(&self) -> &'static str {
        match self {
            Operation::XRPLToCoreum => "xrpl_to_coreum",
        }
    }
}
