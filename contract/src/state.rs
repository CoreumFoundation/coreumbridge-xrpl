use cosmwasm_schema::cw_serde;
use cosmwasm_std::Addr;
use cw_storage_plus::{Item, Map};

/// Top level storage key. Values must not conflict.
/// Each key is only one byte long to ensure we use the smallest possible storage keys.
#[repr(u8)]
pub enum TopKey {
    Config = b'c',
    TokensCoreum = b'1',
    TokensXRPL = b'2',
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
pub struct TokenXRP {
    pub issuer: String,
    pub currency: String,
    pub coreum_denom: String,
}

#[cw_serde]
pub struct TokenCoreum {
    pub denom: String,
    pub decimals: u32,
    pub xprl_currency: String,
}

pub const CONFIG: Item<Config> = Item::new(TopKey::Config.as_str());
//Tokens registered from Coreum side - key is denom on Coreum chain
pub const TOKENS_COREUM: Map<String, TokenCoreum> = Map::new(TopKey::TokensCoreum.as_str());
//Tokens registered from XPRL side - key is issuer+currency on XPRL
pub const TOKENS_XRPL: Map<String, TokenXRP> = Map::new(TopKey::TokensXRPL.as_str());

