use std::collections::HashSet;

use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Deps, Storage};

use crate::{
    address::{check_address_is_not_prohibited, validate_xrpl_address},
    contract::MAX_RELAYERS,
    error::ContractError,
    evidence::TransactionResult,
    state::{CONFIG, PENDING_ROTATE_KEYS, TX_EVIDENCES},
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
    let mut set_xrpl_addresses = HashSet::new();
    let mut set_xrpl_pubkeys = HashSet::new();
    let mut set_coreum_addresses = HashSet::new();

    // Threshold can't be 0 or more than number of relayers
    if evidence_threshold == 0 || evidence_threshold as usize > relayers.len() {
        return Err(ContractError::InvalidThreshold {});
    }

    if relayers.len() > MAX_RELAYERS {
        return Err(ContractError::TooManyRelayers {});
    }

    for relayer in relayers {
        deps.api.addr_validate(relayer.coreum_address.as_ref())?;
        validate_xrpl_address(&relayer.xrpl_address)?;
        check_address_is_not_prohibited(deps.storage, relayer.xrpl_address.clone())?;

        // If the set returns false during insertion it means that the key already exists and therefore is duplicated
        if !set_xrpl_addresses.insert(relayer.xrpl_address.clone()) {
            return Err(ContractError::DuplicatedRelayer {});
        };
        if !set_xrpl_pubkeys.insert(relayer.xrpl_pub_key.clone()) {
            return Err(ContractError::DuplicatedRelayer {});
        };
        if !set_coreum_addresses.insert(relayer.coreum_address.clone()) {
            return Err(ContractError::DuplicatedRelayer {});
        };
    }

    Ok(())
}

pub fn is_relayer(storage: &dyn Storage, sender: &Addr) -> Result<bool, ContractError> {
    let config = CONFIG.load(storage)?;

    Ok(config.relayers.iter().any(|r| r.coreum_address == sender))
}

pub fn handle_rotate_keys_confirmation(
    storage: &mut dyn Storage,
    relayers: Vec<Relayer>,
    new_evidence_threshold: u32,
    transaction_result: &TransactionResult,
) -> Result<(), ContractError> {
    // If transaction was accepted, update the relayers and evidence threshold and clear all current evidences
    // Bridge will stay halted until owner resumes it.
    // If it failed, the bridge will remain halted and relayers are not updated, waiting for another recovery by owner
    if transaction_result.eq(&TransactionResult::Accepted) {
        let mut config = CONFIG.load(storage)?;
        config.relayers = relayers;
        config.evidence_threshold = new_evidence_threshold;
        CONFIG.save(storage, &config)?;
        TX_EVIDENCES.clear(storage);
    }

    PENDING_ROTATE_KEYS.save(storage, &false)?;

    Ok(())
}
