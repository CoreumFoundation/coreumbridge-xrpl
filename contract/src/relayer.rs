use std::collections::HashMap;

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Deps, Empty, Storage};

use crate::{
    contract::MAX_RELAYERS,
    error::ContractError,
    evidence::TransactionResult,
    state::{CONFIG, PENDING_KEY_ROTATION, TX_EVIDENCES},
};

#[cw_serde]
pub struct Relayer {
    pub coreum_address: Addr,
    pub xrpl_address: String,
    pub xrpl_pub_key: String,
}

pub fn validate_relayers(
    deps: Deps,
    relayers: &Vec<Relayer>,
    evidence_threshold: u32,
) -> Result<(), ContractError> {
    let mut map_xrpl_addresses = HashMap::new();
    let mut map_xrpl_pubkeys = HashMap::new();
    let mut map_coreum_addresses = HashMap::new();

    // Threshold can't be 0
    if evidence_threshold == 0 {
        return Err(ContractError::ThresholdZero {});
    }

    // Threshold can't be more than number of relayers
    if evidence_threshold > relayers.len() as u32 {
        return Err(ContractError::InvalidThreshold {});
    }

    if relayers.len() as u32 > MAX_RELAYERS {
        return Err(ContractError::TooManyRelayers {});
    }

    for relayer in relayers.iter() {
        deps.api.addr_validate(relayer.coreum_address.as_ref())?;
        validate_xrpl_address(relayer.xrpl_address.to_owned())?;

        // If the map returns a value during insertion, it means that the key already exists and therefore is duplicated
        if map_xrpl_addresses
            .insert(relayer.xrpl_address.to_owned(), Empty {})
            .is_some()
        {
            return Err(ContractError::DuplicatedRelayerXRPLAddress {});
        };
        if map_xrpl_pubkeys
            .insert(relayer.xrpl_pub_key.to_owned(), Empty {})
            .is_some()
        {
            return Err(ContractError::DuplicatedRelayerXRPLPubKey {});
        };
        if map_coreum_addresses
            .insert(relayer.coreum_address.to_owned(), Empty {})
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

pub fn assert_relayer(deps: Deps, sender: &Addr) -> Result<(), ContractError> {
    let config = CONFIG.load(deps.storage)?;

    if config.relayers.iter().any(|r| r.coreum_address == sender) {
        return Ok(());
    }

    Err(ContractError::UnauthorizedSender {})
}

pub fn handle_key_rotation_confirmation(
    storage: &mut dyn Storage,
    relayers: Vec<Relayer>,
    new_evidence_threshold: u32,
    transaction_result: TransactionResult,
) -> Result<(), ContractError> {
    let mut config = CONFIG.load(storage)?;
    // Set config relayers to the new relayers if the transaction was successful update the relayers and evidence threshhold, and clear all current evidences
    // Bridge will stay halted until owner resumes it.
    // If it failed, the bridge will remain halted and relayers are not updated, waiting for another recovery by owner
    if transaction_result.eq(&TransactionResult::Accepted) {
        config.relayers = relayers;
        config.evidence_threshold = new_evidence_threshold;
        TX_EVIDENCES.clear(storage);
        CONFIG.save(storage, &config)?;
    }

    PENDING_KEY_ROTATION.save(storage, &false)?;

    Ok(())
}
