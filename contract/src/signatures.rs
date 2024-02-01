use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, DepsMut};

use crate::{
    error::ContractError,
    operation::check_valid_operation_if_halt,
    state::{CONFIG, PENDING_OPERATIONS},
};

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
    // We get the current signatures for this specific operation
    let mut pending_operation = PENDING_OPERATIONS
        .load(deps.storage, operation_id)
        .map_err(|_| ContractError::PendingOperationNotFound {})?;

    if operation_version != pending_operation.version {
        return Err(ContractError::OperationVersionMismatch {});
    }

    let config = CONFIG.load(deps.storage)?;

    // If bridge is halted we prohibit all signatures except for allowed operations
    check_valid_operation_if_halt(deps.storage, &config, &pending_operation.operation_type)?;

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
