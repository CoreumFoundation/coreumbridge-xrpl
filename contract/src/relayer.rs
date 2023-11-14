use std::collections::HashMap;

use coreum_wasm_sdk::core::CoreumQueries;
use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Deps, DepsMut, Empty};

use crate::{error::ContractError, state::CONFIG};

#[cw_serde]
pub struct Relayer {
    pub coreum_address: Addr,
    pub xrpl_address: String,
    pub xrpl_pub_key: String,
}

pub fn validate_relayers(
    deps: &DepsMut<CoreumQueries>,
    relayers: Vec<Relayer>,
) -> Result<(), ContractError> {
    let mut map_xrpl_addresses = HashMap::new();
    let mut map_xrpl_pubkeys = HashMap::new();
    let mut map_coreum_addresses = HashMap::new();

    for relayer in relayers {
        deps.api.addr_validate(relayer.coreum_address.as_ref())?;
        validate_xrpl_address(relayer.xrpl_address.clone())?;

        // If the map returns a value during insertion, it means that the key already exists and therefore is duplicated
        if map_xrpl_addresses
            .insert(relayer.xrpl_address, Empty {})
            .is_some()
        {
            return Err(ContractError::DuplicatedRelayerXRPLAddress {});
        };
        if map_xrpl_pubkeys
            .insert(relayer.xrpl_pub_key, Empty {})
            .is_some()
        {
            return Err(ContractError::DuplicatedRelayerXRPLPubKey {});
        };
        if map_coreum_addresses
            .insert(relayer.coreum_address, Empty {})
            .is_some()
        {
            return Err(ContractError::DuplicatedRelayerCoreumAddress {});
        };
    }

    Ok(())
}

pub fn validate_xrpl_address(address: String) -> Result<(), ContractError> {
    // We validate that the length of the issuer is between 24 and 34 characters and starts with 'r'
    if address.len() >= 25
        && address.len() <= 35
        && address.starts_with('r')
        && address
            .chars()
            .all(|c| c.is_alphanumeric() && c != '0' && c != 'O' && c != 'I' && c != 'l')
    {
        return Ok(());
    }
    Err(ContractError::InvalidXRPLAddress { address })
}

pub fn assert_relayer(deps: Deps, sender: Addr) -> Result<(), ContractError> {
    let config = CONFIG.load(deps.storage)?;

    if config.relayers.iter().any(|r| r.coreum_address == sender) {
        return Ok(());
    }

    Err(ContractError::UnauthorizedSender {})
}
