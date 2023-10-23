use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage, Uint128};
use sha2::{Digest, Sha256};

use crate::{
    error::ContractError,
    state::{Evidences, CONFIG, PROCESSED_TXS, TX_EVIDENCES},
};

#[cw_serde]
pub enum Evidence {
    #[serde(rename = "xrpl_to_coreum_transfer")]
    XRPLToCoreumTransfer {
        tx_hash: String,
        issuer: String,
        currency: String,
        amount: Uint128,
        recipient: Addr,
    },
    //This type will be used for ANY transaction that comes from XRPL and that is notifying a confirmation or rejection.
    #[serde(rename = "xrpl_transaction_result")]
    XRPLTransactionResult {
        tx_hash: Option<String>,
        sequence_number: Option<u64>,
        ticket_number: Option<u64>,
        //true if confirmed, false if rejected
        confirmed: bool,
        //If an operation is invalid, we will not not register it as processed
        valid: bool,
        operation_result: OperationResult,
    },
}

#[cw_serde]
pub enum OperationResult {
    TicketsAllocation { tickets: Option<Vec<u64>> },
}

//For convenience in the responses.
impl OperationResult {
    pub fn as_str(&self) -> &'static str {
        match self {
            OperationResult::TicketsAllocation { .. } => "tickets_allocation",
        }
    }
}

impl Evidence {
    //We hash the entire Evidence struct to avoid having to deal with different types of hashes
    pub fn get_hash(self) -> String {
        let to_hash_bytes = serde_json::to_string(&self).unwrap().into_bytes();
        hash_bytes(to_hash_bytes)
    }

    pub fn get_tx_hash(self) -> String {
        match self {
            Evidence::XRPLToCoreumTransfer { tx_hash, .. } => tx_hash,
            Evidence::XRPLTransactionResult { tx_hash, .. } => tx_hash.unwrap(),
        }
        .to_lowercase()
    }
    pub fn is_operation_valid(self) -> bool {
        match self {
            Evidence::XRPLToCoreumTransfer { .. } => true,
            Evidence::XRPLTransactionResult { valid, .. } => valid,
        }
    }
    //Function for basic validation of evidences in case relayers send something that is not valid
    pub fn validate(self) -> Result<(), ContractError> {
        match self {
            Evidence::XRPLToCoreumTransfer { amount, .. } => {
                if amount.u128() == 0 {
                    return Err(ContractError::InvalidAmount {});
                }
                Ok(())
            }
            Evidence::XRPLTransactionResult {
                tx_hash,
                sequence_number,
                ticket_number,
                confirmed,
                valid,
                operation_result,
            } => {
                if (sequence_number.is_none() && ticket_number.is_none())
                    || (sequence_number.is_some() && ticket_number.is_some())
                {
                    return Err(ContractError::InvalidTransactionResultEvidence {});
                }

                // Valid transactions must have a tx_hash
                if valid && tx_hash.is_none() {
                    return Err(ContractError::InvalidValidTransactionResultEvidence {});
                }

                // Invalid transactions can't have a tx_hash or be confirmed
                if !valid && (tx_hash.is_some() || confirmed) {
                    return Err(ContractError::InvalidNotValidTransactionResultEvidence {});
                }

                match operation_result {
                    
                    OperationResult::TicketsAllocation { tickets } => {
                        //Invalid or unconfirmed transactions should not contain tickets
                        if (!valid || !confirmed) && tickets.is_some() {
                            return Err(ContractError::InvalidTicketAllocationEvidence {});
                        }
                        //We can't confirm an operation that allocates no tickets
                        if confirmed && (tickets.is_none() || tickets.unwrap().is_empty()) {
                            return Err(ContractError::InvalidTicketAllocationEvidence {});
                        }
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
    let operation_valid = evidence.clone().is_operation_valid();

    if operation_valid && PROCESSED_TXS.has(storage, evidence.clone().get_tx_hash()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    match TX_EVIDENCES.may_load(storage, evidence.clone().get_hash())? {
        Some(stored_evidences) => {
            if stored_evidences.relayers.contains(&sender) {
                return Err(ContractError::EvidenceAlreadyProvided {});
            }
            evidences = stored_evidences;
            evidences.relayers.push(sender)
        }
        None => {
            evidences = Evidences {
                relayers: vec![sender],
            };
        }
    }

    let config = CONFIG.load(storage)?;
    if evidences.relayers.len() >= config.evidence_threshold.try_into().unwrap() {
        // We only registered the transaction as processed if its execution didn't fail
        if operation_valid {
            PROCESSED_TXS.save(storage, evidence.clone().get_tx_hash(), &Empty {})?;
        }
        // if there is just one relayer there is nothing to delete
        if evidences.relayers.len() != 1 {
            TX_EVIDENCES.remove(storage, evidence.get_hash());
        }
        return Ok(true);
    }

    TX_EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
