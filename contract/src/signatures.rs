use cosmwasm_std::{Addr, DepsMut};

use crate::{
    error::ContractError,
    state::{Signature, PENDING_OPERATIONS},
};

pub fn add_signature(
    deps: DepsMut,
    operation_id: u64,
    sender: Addr,
    signature: String,
) -> Result<(), ContractError> {
    //We get the current signatures for this specific operation
    let mut pending_operation = PENDING_OPERATIONS
        .load(deps.storage, operation_id)
        .map_err(|_| ContractError::PendingOperationNotFound {})?;

    let mut signatures = pending_operation.signatures;

    //If this relayer already provided a signature he can't overwrite it
    if signatures.clone().into_iter().any(
        |Signature {
             relayer,
             signature: _,
         }| relayer == sender,
    ) {
        return Err(ContractError::SignatureAlreadyProvided {});
    }

    //Add signature and store it
    signatures.push(Signature {
        relayer: sender,
        signature,
    });

    pending_operation.signatures = signatures;
    PENDING_OPERATIONS.save(deps.storage, operation_id, &pending_operation)?;

    Ok(())
}
