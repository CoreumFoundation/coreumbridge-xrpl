use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Empty, Storage, Uint128};
use sha2::{Digest, Sha256};

use crate::{
    error::ContractError,
    state::{CONFIG, PROCESSED_TXS, TX_EVIDENCES},
};

#[cw_serde]
pub enum Evidence {
    // This evidence is only used for token transfers from XRPL to Coreum
    #[serde(rename = "xrpl_to_coreum_transfer")]
    XRPLToCoreumTransfer {
        tx_hash: String,
        issuer: String,
        currency: String,
        amount: Uint128,
        recipient: Addr,
    },
    // This type will be used for ANY transaction that comes from XRPL and that is notifying a confirmation or rejection
    #[serde(rename = "xrpl_transaction_result")]
    XRPLTransactionResult {
        tx_hash: Option<String>,
        account_sequence: Option<u64>,
        ticket_sequence: Option<u64>,
        transaction_result: TransactionResult,
        operation_result: Option<OperationResult>,
    },
}

#[cw_serde]
pub enum TransactionResult {
    // Transactions that were accepted in XRPL and have their corresponding Transaction Hash
    Accepted,
    // Transactions that were rejected in XRPL and have their corresponding Transaction Hash
    Rejected,
    // These transactions have no transaction hash because they couldn't be processed in XRPL
    Invalid,
}

// For convenience in the responses
impl TransactionResult {
    pub const fn as_str(&self) -> &'static str {
        match self {
            Self::Accepted => "transaction_accepted",
            Self::Rejected => "transaction_rejected",
            Self::Invalid => "transaction_invalid",
        }
    }
}

#[cw_serde]
pub enum OperationResult {
    TicketsAllocation { tickets: Option<Vec<u64>> },
}

impl Evidence {
    // We hash the entire Evidence struct to avoid having to deal with different types of hashes
    pub fn get_hash(&self) -> String {
        let to_hash_bytes = serde_json::to_string(self).unwrap().into_bytes();
        hash_bytes(to_hash_bytes)
    }

    pub fn get_tx_hash(&self) -> String {
        match self {
            Self::XRPLToCoreumTransfer { tx_hash, .. } => tx_hash.clone(),
            Self::XRPLTransactionResult { tx_hash, .. } => tx_hash.clone().unwrap(),
        }
        .to_uppercase()
    }
    pub fn is_operation_valid(&self) -> bool {
        match self {
            // All transfers are valid operations
            Self::XRPLToCoreumTransfer { .. } => true,
            // All rejected/confirmed transactions are valid operations
            Self::XRPLTransactionResult {
                transaction_result, ..
            } => transaction_result.clone() != TransactionResult::Invalid,
        }
    }
    // Function for basic validation of evidences in case relayers send something that is not valid
    pub fn validate_basic(&self) -> Result<(), ContractError> {
        match self {
            Self::XRPLToCoreumTransfer { amount, .. } => {
                if amount.is_zero() {
                    return Err(ContractError::InvalidAmount {});
                }
                Ok(())
            }
            Self::XRPLTransactionResult {
                tx_hash,
                account_sequence,
                ticket_sequence,
                transaction_result,
                operation_result,
            } => {
                // A transaction result can only have an account sequence or a ticket sequence, not both
                if (account_sequence.is_none() && ticket_sequence.is_none())
                    || (account_sequence.is_some() && ticket_sequence.is_some())
                {
                    return Err(ContractError::InvalidTransactionResultEvidence {});
                }

                // Valid transactions must have a tx_hash
                if transaction_result.ne(&TransactionResult::Invalid) && tx_hash.is_none() {
                    return Err(ContractError::InvalidSuccessfulTransactionResultEvidence {});
                }

                // Invalid transactions can't have a tx_hash
                if transaction_result.eq(&TransactionResult::Invalid) && tx_hash.is_some() {
                    return Err(ContractError::InvalidFailedTransactionResultEvidence {});
                }

                match operation_result {
                    Some(OperationResult::TicketsAllocation { tickets }) => {
                        // If a transaction is invalid or rejected, we can't provide tickets in the operation result
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
                    None => {}
                }

                Ok(())
            }
        }
    }
}

#[cw_serde]
pub struct Evidences {
    pub relayer_coreum_addresses: Vec<Addr>,
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
    evidence: &Evidence,
) -> Result<bool, ContractError> {
    let operation_valid = evidence.is_operation_valid();

    if operation_valid && PROCESSED_TXS.has(storage, evidence.get_tx_hash()) {
        return Err(ContractError::OperationAlreadyExecuted {});
    }

    let mut evidences: Evidences;
    // Relayers can only provide the evidence once
    match TX_EVIDENCES.may_load(storage, evidence.get_hash())? {
        Some(stored_evidences) => {
            if stored_evidences.relayer_coreum_addresses.contains(&sender) {
                return Err(ContractError::EvidenceAlreadyProvided {});
            }
            evidences = stored_evidences;
            evidences.relayer_coreum_addresses.push(sender);
        }
        None => {
            evidences = Evidences {
                relayer_coreum_addresses: vec![sender],
            };
        }
    }

    let config = CONFIG.load(storage)?;
    if evidences.relayer_coreum_addresses.len() >= config.evidence_threshold as usize {
        // We only registered the transaction as processed if its execution didn't fail (it wasn't Invalid)
        if operation_valid {
            PROCESSED_TXS.save(storage, evidence.get_tx_hash(), &Empty {})?;
        }
        // If there is just one relayer there is nothing to delete
        if evidences.relayer_coreum_addresses.len() != 1 {
            TX_EVIDENCES.remove(storage, evidence.get_hash());
        }
        return Ok(true);
    }

    TX_EVIDENCES.save(storage, evidence.get_hash(), &evidences)?;

    Ok(false)
}
