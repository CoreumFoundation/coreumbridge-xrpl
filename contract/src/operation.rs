use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Storage, Uint128};

use crate::{
    contract::build_xrpl_token_key,
    error::ContractError,
    evidence::TransactionResult,
    signatures::Signature,
    state::{TokenState, PENDING_OPERATIONS, XRPL_TOKENS},
};

#[cw_serde]
pub struct Operation {
    pub ticket_sequence: Option<u64>,
    pub account_sequence: Option<u64>,
    pub signatures: Vec<Signature>,
    pub operation_type: OperationType,
}

#[cw_serde]
pub enum OperationType {
    AllocateTickets {
        number: u32,
    },
    TrustSet {
        issuer: String,
        currency: String,
        trust_set_limit_amount: Uint128,
    },
    #[serde(rename = "coreum_to_xrpl_transfer")]
    CoreumToXRPLTransfer {
        issuer: String,
        currency: String,
        amount: Uint128,
        recipient: String,
    },
}

pub fn check_operation_exists(
    storage: &mut dyn Storage,
    account_sequence: Option<u64>,
    ticket_sequence: Option<u64>,
) -> Result<u64, ContractError> {
    // Get the sequence or ticket number (priority for sequence number)
    let operation_id = account_sequence.unwrap_or(ticket_sequence.unwrap_or_default());

    if !PENDING_OPERATIONS.has(storage, operation_id) {
        return Err(ContractError::PendingOperationNotFound {});
    }

    Ok(operation_id)
}

pub fn check_and_save_pending_operation(
    storage: &mut dyn Storage,
    operation_id: u64,
    operation: &Operation,
) -> Result<(), ContractError> {
    if PENDING_OPERATIONS.has(storage, operation_id) {
        return Err(ContractError::PendingOperationAlreadyExists {});
    }
    PENDING_OPERATIONS.save(storage, operation_id, operation)?;

    Ok(())
}

pub fn handle_trust_set_confirmation(
    storage: &mut dyn Storage,
    issuer: String,
    currency: String,
    transaction_result: TransactionResult,
) -> Result<(), ContractError> {
    let key = build_xrpl_token_key(issuer, currency);

    let mut token = XRPL_TOKENS
        .load(storage, key.clone())
        .map_err(|_| ContractError::TokenNotRegistered {})?;

    // Set token to active if TrustSet operation was successful
    if transaction_result.eq(&TransactionResult::Accepted) {
        token.state = TokenState::Enabled;
    } else {
        token.state = TokenState::Inactive;
    }

    XRPL_TOKENS.save(storage, key, &token)?;
    Ok(())
}

pub fn create_operation(
    ticket_sequence: Option<u64>,
    account_sequence: Option<u64>,
    operation_type: OperationType,
) -> Operation {
    Operation {
        ticket_sequence,
        account_sequence,
        signatures: vec![],
        operation_type,
    }
}
