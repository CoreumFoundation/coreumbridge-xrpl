use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage, Uint128};
use sha2::{Digest, Sha256};

use crate::{
    error::ContractError,
    state::{Evidences, CONFIG, EVIDENCES, EXECUTED_EVIDENCE_OPERATIONS},
};

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
            Evidence::XRPLToCoreum {
                tx_hash,
                issuer,
                currency,
                amount,
                recipient,
            } => {
                let to_hash = format!("{}{}{}{}{}", tx_hash, issuer, currency, amount, recipient,)
                    .into_bytes();
                hash_bytes(to_hash)
            }
        }
    }
    pub fn get_tx_hash(&self) -> String {
        match self {
            Evidence::XRPLToCoreum { tx_hash, .. } => tx_hash.clone(),
        }
    }
    pub fn validate(&self) -> Result<(), ContractError> {
        match self {
            Evidence::XRPLToCoreum { amount, .. } => {
                if amount.u128() == 0 {
                    return Err(ContractError::InvalidAmount {});
                }
                Ok(())
            }
        }
    }
}

pub fn hash_bytes(bytes: Vec<u8>) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    let output = hasher.finalize();
    hex::encode(output)
}

// this func needs explanation in comments.
pub fn handle_evidence(
    storage: &mut dyn Storage,
    sender: Addr,
    evidence: Evidence,
) -> Result<bool, ContractError> {
    if EXECUTED_EVIDENCE_OPERATIONS.has(storage, evidence.get_tx_hash().to_lowercase()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    match EVIDENCES.may_load(storage, evidence.get_hash())? { // EVIDENCES are evidences for pending txs right ?
        Some(stored_evidences) => {
            if stored_evidences.relayers.contains(&sender) {
                return Err(ContractError::EvidenceAlreadyProvided {});
            }
            evidences = stored_evidences;
            evidences.relayers.push(sender.clone())
        }
        None => {
            evidences = Evidences {
                relayers: vec![sender.clone()],
            };
        }
    }

    let config = CONFIG.load(storage)?;
    if evidences.relayers.len() >= config.evidence_threshold.try_into().unwrap() {
        EXECUTED_EVIDENCE_OPERATIONS.save(
            storage,
            evidence.get_tx_hash().to_lowercase(),
            &Empty {},
        )?;
        // if there is just one relayer there is nothing to delete
        if evidences.relayers.len() != 1 {
            EVIDENCES.remove(storage, evidence.get_hash());
        }
        return Ok(true);
    }

    EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
