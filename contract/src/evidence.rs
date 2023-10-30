use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage, Uint128};
use sha2::{Digest, Sha256};

use crate::{
    error::ContractError,
    state::{CONFIG, PROCESSED_TXS, TX_EVIDENCES},
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
    // This type will be used for ANY transaction that comes from XRPL and that is notifying a confirmation or rejection.
    #[serde(rename = "xrpl_transaction_result")]
    XRPLTransactionResult {
        tx_hash: Option<String>,
        sequence_number: Option<u64>,
        ticket_number: Option<u64>,
        transaction_result: TransactionResult,
        operation_result: OperationResult,
    },
}

#[cw_serde]
pub enum TransactionResult {
    Accepted,
    Rejected,
    Invalid,
}

// For convenience in the responses.
impl TransactionResult {
    pub fn as_str(&self) -> &'static str {
        match self {
            TransactionResult::Accepted => "transaction_accepted",
            TransactionResult::Rejected => "transaction_rejected",
            TransactionResult::Invalid => "transaction_invalid",
        }
    }
}

#[cw_serde]
pub enum OperationResult {
    TicketsAllocation {
        tickets: Option<Vec<u64>>,
    },
    TrustSet {
        issuer: Option<String>,
        currency: Option<String>,
    },
}

// For convenience in the responses.
impl OperationResult {
    pub fn as_str(&self) -> &'static str {
        match self {
            OperationResult::TicketsAllocation { .. } => "tickets_allocation",
            OperationResult::TrustSet { .. } => "trust_set",
        }
    }
}

impl Evidence {
    // We hash the entire Evidence struct to avoid having to deal with different types of hashes
    pub fn get_hash(&self) -> String {
        let to_hash_bytes = serde_json::to_string(self).unwrap().into_bytes();
        hash_bytes(to_hash_bytes)
    }

    pub fn get_tx_hash(&self) -> String {
        match self {
            Evidence::XRPLToCoreumTransfer { tx_hash, .. } => tx_hash.clone(),
            Evidence::XRPLTransactionResult { tx_hash, .. } => tx_hash.clone().unwrap(),
        }
        .to_lowercase()
    }
    pub fn is_operation_valid(&self) -> bool {
        match self {
            Evidence::XRPLToCoreumTransfer { .. } => true,
            Evidence::XRPLTransactionResult {
                transaction_result, ..
            } => transaction_result.clone() != TransactionResult::Invalid,
        }
    }
    // Function for basic validation of evidences in case relayers send something that is not valid
    pub fn validate(&self) -> Result<(), ContractError> {
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
                transaction_result,
                operation_result,
            } => {
                if (sequence_number.is_none() && ticket_number.is_none())
                    || (sequence_number.is_some() && ticket_number.is_some())
                {
                    return Err(ContractError::InvalidTransactionResultEvidence {});
                }

                // Valid transactions must have a tx_hash
                if transaction_result.ne(&TransactionResult::Invalid) && tx_hash.is_none() {
                    return Err(ContractError::InvalidValidTransactionResultEvidence {});
                }

                // Invalid transactions can't have a tx_hash
                if transaction_result.eq(&TransactionResult::Invalid) && tx_hash.is_some() {
                    return Err(ContractError::InvalidNotValidTransactionResultEvidence {});
                }

                match operation_result {
                    OperationResult::TicketsAllocation { tickets } => {
                        // Invalid or rejected transactions should not contain tickets
                        if (transaction_result.eq(&TransactionResult::Invalid)
                            || transaction_result.eq(&TransactionResult::Rejected))
                            && tickets.is_some()
                        {
                            return Err(ContractError::InvalidTicketAllocationEvidence {});
                        }
                        // We can't accept an operation that allocates no tickets
                        if transaction_result.eq(&TransactionResult::Accepted)
                            && (tickets.is_none() || tickets.as_ref().unwrap().is_empty())
                        {
                            return Err(ContractError::InvalidTicketAllocationEvidence {});
                        }
                    }
                    OperationResult::TrustSet { issuer, currency } => {
                        // Invalid or rejected transactions should not have issuer or currency
                        if (transaction_result.eq(&TransactionResult::Invalid)
                            || transaction_result.eq(&TransactionResult::Rejected))
                            && (issuer.is_some() || currency.is_some())
                        {
                            return Err(ContractError::InvalidTrustSetEvidence {});
                        }

                        // Accepted transactions must have issuer and currency
                        if transaction_result.eq(&TransactionResult::Accepted)
                            && (issuer.is_none()
                                || issuer.as_ref().unwrap().is_empty()
                                || currency.is_none()
                                || currency.as_ref().unwrap().is_empty())
                        {
                            return Err(ContractError::InvalidTrustSetEvidence {});
                        }
                    }
                }
                Ok(())
            }
        }
    }
}

#[cw_serde]
pub struct Evidences {
    pub relayers: Vec<Addr>,
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
    let operation_valid = evidence.is_operation_valid();

    if operation_valid && PROCESSED_TXS.has(storage, evidence.get_tx_hash()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    match TX_EVIDENCES.may_load(storage, evidence.get_hash())? {
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
            PROCESSED_TXS.save(storage, evidence.get_tx_hash(), &Empty {})?;
        }
        // If there is just one relayer there is nothing to delete
        if evidences.relayers.len() != 1 {
            TX_EVIDENCES.remove(storage, evidence.get_hash());
        }
        return Ok(true);
    }

    TX_EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
