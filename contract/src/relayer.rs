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
    let number_of_relayers = relayers.len();

    for relayer in relayers {
        deps.api.addr_validate(relayer.coreum_address.as_ref())?;
        validate_xrpl_address(relayer.xrpl_address.clone())?;

        // Store all values in maps so we can easily verify if there are duplicates.
        map_xrpl_addresses.insert(relayer.xrpl_address, Empty {});
        map_xrpl_pubkeys.insert(relayer.xrpl_pub_key, Empty {});
        map_coreum_addresses.insert(relayer.coreum_address, Empty {});
    }

    if map_xrpl_addresses.len() != number_of_relayers {
        return Err(ContractError::DuplicatedRelayerXRPLAddress {});
    }

    if map_xrpl_pubkeys.len() != number_of_relayers {
        return Err(ContractError::DuplicatedRelayerXRPLPubKey {});
    }

    if map_coreum_addresses.len() != number_of_relayers {
        return Err(ContractError::DuplicatedRelayerCoreumAddress {});
    }

    Ok(())
}

pub fn validate_xrpl_address(address: String) -> Result<(), ContractError> {
    // We validate that the length of the issuer is between 24 and 34 characters and starts with 'r'
    if address.len() >= 24 && address.len() <= 34 && address.starts_with('r') {
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
