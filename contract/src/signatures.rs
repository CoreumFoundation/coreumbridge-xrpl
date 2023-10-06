use cosmwasm_std::{Addr, DepsMut};

use crate::{
    error::ContractError,
    state::{Signature, PENDING_OPERATIONS},
};

pub fn add_signature(
    deps: DepsMut,
    number: u64,
    sender: Addr,
    signature: String,
) -> Result<(), ContractError> {
    if !PENDING_OPERATIONS.has(deps.storage, number) {
        return Err(ContractError::PendingOperationNotFound {});
    }
    //We get the current signatures for this specific operation
    let mut pending_operation = PENDING_OPERATIONS.load(deps.storage, number)?;
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
        relayer: sender.clone(),
        signature: signature.clone(),
    });

    pending_operation.signatures = signatures;
    PENDING_OPERATIONS.save(deps.storage, number, &pending_operation)?;

    Ok(())
}
