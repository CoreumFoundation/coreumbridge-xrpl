use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, DepsMut};

use crate::{error::ContractError, state::PENDING_OPERATIONS};

const MAX_SIGNATURE_LENGTH: usize = 200;

#[cw_serde]
pub struct Signature {
    pub relayer_coreum_address: Addr,
    pub signature: String,
}

pub fn add_signature(
    deps: DepsMut,
    operation_id: u64,
    operation_version: u64,
    sender: Addr,
    signature: String,
) -> Result<(), ContractError> {
    validate_signature(&signature)?;

    // We get the current signatures for this specific operation
    let mut pending_operation = PENDING_OPERATIONS
        .load(deps.storage, operation_id)
        .map_err(|_| ContractError::PendingOperationNotFound {})?;

    if operation_version != pending_operation.version {
        return Err(ContractError::OperationVersionMismatch {});
    }

    let mut signatures = pending_operation.signatures;

    // If this relayer already provided a signature he can't overwrite it
    if signatures.clone().into_iter().any(
        |Signature {
             relayer_coreum_address,
             signature: _,
         }| relayer_coreum_address == sender,
    ) {
        return Err(ContractError::SignatureAlreadyProvided {});
    }

    // Add signature and store it
    signatures.push(Signature {
        relayer_coreum_address: sender,
        signature,
    });

    pending_operation.signatures = signatures;
    PENDING_OPERATIONS.save(deps.storage, operation_id, &pending_operation)?;

    Ok(())
}

fn validate_signature(signature: &String) -> Result<(), ContractError> {
    // The purpose of this function is to avoid attacks
    // We set a max length of 200, a reasonable length, here to avoid spam attack by a malicious relayer that wants to send a very long signature for an operation
    // And to also not error out in case a relayer sends a signature that is a bit longer than the one we expect
    if signature.len() > MAX_SIGNATURE_LENGTH {
        return Err(ContractError::InvalidSignatureLength {});
    }
    Ok(())
}
