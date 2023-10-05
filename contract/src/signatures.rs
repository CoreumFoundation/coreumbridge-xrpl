use cosmwasm_std::{Addr, DepsMut, Storage};

use crate::{
    error::ContractError,
    state::{Signature, EXECUTED_OPERATION_SIGNATURES, PENDING_OPERATIONS, SIGNING_QUEUE},
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

    let mut signatures;
    match SIGNING_QUEUE.may_load(deps.storage, number)? {
        Some(stored_signatures) => {
            if stored_signatures.clone().into_iter().any(
                |Signature {
                     relayer,
                     signature: _,
                 }| relayer == sender,
            ) {
                return Err(ContractError::SignatureAlreadyProvided {});
            }
            signatures = stored_signatures;
        }
        None => {
            signatures = vec![];
        }
    }

    signatures.push(Signature {
        relayer: sender.clone(),
        signature: signature.clone(),
    });
    SIGNING_QUEUE.save(deps.storage, number, &signatures)?;

    Ok(())
}

// Moves signatures of a confirmed operation from SIGNING_QUEUE to EXECUTED_OPERATION_SIGNATURES
pub fn confirm_operation_signatures(
    storage: &mut dyn Storage,
    number: u64,
) -> Result<(), ContractError> {
    let signatures = SIGNING_QUEUE.load(storage, number)?;
    EXECUTED_OPERATION_SIGNATURES.save(storage, number, &signatures)?;
    SIGNING_QUEUE.remove(storage, number);

    Ok(())
}
