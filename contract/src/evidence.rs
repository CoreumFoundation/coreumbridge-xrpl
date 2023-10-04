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
    TicketAllocation {
        tx_hash: String,
        sequence_number: Option<u64>,
        ticket_number: Option<u64>,
        //true if confirmed, false if rejected
        tickets: Option<Vec<u64>>,
        confirmed: bool,
    },
}

impl Evidence {
    pub fn get_hash(self) -> String {
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
            Evidence::TicketAllocation {
                tx_hash,
                sequence_number,
                ticket_number,
                confirmed,
                ..
            } => {
                let to_hash = format!(
                    "{}{}{}{}",
                    tx_hash,
                    sequence_number.unwrap_or_default(),
                    ticket_number.unwrap_or_default(),
                    confirmed,
                )
                .into_bytes();
                hash_bytes(to_hash)
            }
        }
    }
    pub fn get_tx_hash(self) -> String {
        match self {
            Evidence::XRPLToCoreum { tx_hash, .. } => tx_hash.clone(),
            Evidence::TicketAllocation { tx_hash, .. } => tx_hash.clone(),
        }
    }
    pub fn validate(self) -> Result<(), ContractError> {
        match self {
            Evidence::XRPLToCoreum { amount, .. } => {
                if amount.u128() == 0 {
                    return Err(ContractError::InvalidAmount {});
                }
                Ok(())
            }
            Evidence::TicketAllocation {
                sequence_number,
                ticket_number,
                tickets,
                confirmed,
                ..
            } => {
                if confirmed {
                    if sequence_number.is_none() && ticket_number.is_none() {
                        return Err(ContractError::InvalidTicketAllocationEvidence {});
                    }
                    if tickets.is_none() || tickets.unwrap().is_empty() {
                        return Err(ContractError::InvalidTicketAllocationEvidence {});
                    }
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

pub fn handle_evidence(
    storage: &mut dyn Storage,
    sender: Addr,
    evidence: Evidence,
) -> Result<bool, ContractError> {
    if EXECUTED_EVIDENCE_OPERATIONS.has(storage, evidence.clone().get_tx_hash().to_lowercase()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    match EVIDENCES.may_load(storage, evidence.clone().get_hash())? {
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
            evidence.clone().get_tx_hash().to_lowercase(),
            &Empty {},
        )?;
        // if there is just one relayer there is nothing to delete
        if evidences.relayers.len() != 1 {
            EVIDENCES.remove(storage, evidence.clone().get_hash());
        }
        return Ok(true);
    }

    EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
