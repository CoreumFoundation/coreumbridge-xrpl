use cosmwasm_std::StdError;
use cw_ownable::OwnershipError;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error(transparent)]
    Ownership(#[from] OwnershipError),

    #[error("Threshold can not be higher than amount of relayers")]
    InvalidThreshold {},

    #[error("Token {} already registered", denom)]
    TokenAlreadyRegistered { denom: String },
}
