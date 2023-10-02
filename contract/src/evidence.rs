use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Uint128, Addr, DepsMut};
use sha2::{Digest, Sha256};

use crate::{error::ContractError, state::{EXECUTED_OPERATIONS, CONFIG, EVIDENCES, Evidences, Operation}};

#[cw_serde]
pub enum Evidence {
    XRPLToCoreum {
        tx_hash: String,
        issuer: String,
        currency: String,
        amount: Uint128,
        recipient: Addr,
    },
}

impl Evidence {
    pub fn get_hash(&self) -> String {
        match self {
            Evidence::XRPLToCoreum { tx_hash, issuer, currency, amount, recipient } => {
                let to_hash = format!(
                    "{}{}{}{}{}{}",
                    tx_hash,
                    issuer,
                    currency,
                    amount,
                    recipient,
                    Operation::XRPLToCoreum.as_str()
                )
                .into_bytes();
                hash_bytes(to_hash)
            },
        }
    }
    pub fn get_tx_hash(&self) -> String {
        match self {
            Evidence::XRPLToCoreum { tx_hash, .. } => tx_hash.clone().to_lowercase(),
        }    
    }
}

pub fn hash_bytes(bytes: Vec<u8>) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    let output = hasher.finalize();
    hex::encode(output)
}

pub fn handle_evidence(deps: DepsMut, sender: Addr, evidence: String, tx_hash: String) -> Result<bool, ContractError> {
    let mut threshold_reached = false;

    if EXECUTED_OPERATIONS.has(deps.storage, evidence.clone()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }
    let config = CONFIG.load(deps.storage)?;
    // Get the evidences that we already have of this current operation
    let evidences = EVIDENCES.may_load(deps.storage, evidence.clone())?;

    //There are already evidences from previous relayers
    if let Some(mut evidences) = evidences {
        if evidences.relayers.contains(&sender) {
            return Err(ContractError::EvidenceAlreadyProvided {});
        }

        if evidences.relayers.len() + 1 == config.evidence_threshold as usize {
            //We have enough evidences, we can execute the operation
            EXECUTED_OPERATIONS.save(deps.storage, evidence.clone(), &tx_hash)?;
            EVIDENCES.remove(deps.storage, evidence.clone());
            //Threshold is reached
            threshold_reached = true;
        } else {
            evidences.relayers.push(sender.clone());
            EVIDENCES.save(deps.storage, evidence.clone(), &evidences)?;
        }
    //First relayer to provide evidence
    } else if config.evidence_threshold == 1 {
        //We have enough evidences, we can execute the operation
        EXECUTED_OPERATIONS.save(deps.storage, evidence.clone(), &tx_hash)?;
        //Threshold is reached
        threshold_reached = true;
    } else {
        let evidences = Evidences {
            relayers: vec![sender.clone()],
        };
        EVIDENCES.save(deps.storage, evidence.clone(), &evidences)?;
    }

    Ok(threshold_reached)
}
