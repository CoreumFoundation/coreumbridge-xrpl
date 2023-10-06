use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage, Uint128};
use sha2::{Digest, Sha256};

use crate::{
    contract::check_operation_exists,
    error::ContractError,
    state::{Evidences, CONFIG, PROCESSED_TXS, TX_EVIDENCES},
    tickets::handle_allocation_confirmation,
};

#[cw_serde]
pub enum Evidence {
    XRPLToCoreumTransfer {
        tx_hash: String,
        issuer: String,
        currency: String,
        amount: Uint128,
        recipient: Addr,
    },
    //This type will be used for ANY transaction that comes from XRPL and that is notifying a confirmation or rejection.
    XRPLTransactionResult {
        tx_hash: String,
        sequence_number: Option<u64>,
        ticket_number: Option<u64>,
        //true if confirmed, false if rejected
        confirmed: bool,
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
    //for each type of evidence we will generate the hash with different values
    pub fn get_hash(self) -> String {
        match self {
            _ => {
                let to_hash_bytes = serde_json::to_string(&self).unwrap().into_bytes();
                hash_bytes(to_hash_bytes)
            }
        }
    }
    pub fn get_tx_hash(self) -> String {
        match self {
            Evidence::XRPLToCoreumTransfer { tx_hash, .. }
            | Evidence::XRPLTransactionResult { tx_hash, .. } => tx_hash.clone(),
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
                sequence_number,
                ticket_number,
                confirmed,
                operation_result,
                ..
            } => {
                //We must always send a sequence or ticket number
                if sequence_number.is_none() && ticket_number.is_none() {
                    return Err(ContractError::InvalidTicketAllocationEvidence {});
                }

                match operation_result {
                    //We can't confirm an operation that allocates no tickets
                    OperationResult::TicketsAllocation { tickets } => {
                        if confirmed && (tickets.is_none() || tickets.unwrap().is_empty()) {
                            return Err(ContractError::InvalidTicketAllocationEvidence {});
                        }
                    }
                }
                Ok(())
            }
        }
    }

    pub fn handle_threshold_confirmation(
        self,
        storage: &mut dyn Storage,
    ) -> Result<(), ContractError> {
        match self {
            Evidence::XRPLToCoreumTransfer { .. } => Ok(()),
            Evidence::XRPLTransactionResult {
                sequence_number,
                ticket_number,
                confirmed,
                operation_result,
                ..
            } => {
                let operation_id = check_operation_exists(storage, sequence_number, ticket_number)?;

                match operation_result {
                    OperationResult::TicketsAllocation { tickets } => {
                        handle_allocation_confirmation(storage, operation_id, tickets, confirmed)?;
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
    if PROCESSED_TXS.has(storage, evidence.clone().get_tx_hash().to_lowercase()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    match TX_EVIDENCES.may_load(storage, evidence.clone().get_hash())? {
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
        PROCESSED_TXS.save(
            storage,
            evidence.clone().get_tx_hash().to_lowercase(),
            &Empty {},
        )?;
        // if there is just one relayer there is nothing to delete
        if evidences.relayers.len() != 1 {
            TX_EVIDENCES.remove(storage, evidence.clone().get_hash());
        }
        return Ok(true);
    }

    TX_EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
