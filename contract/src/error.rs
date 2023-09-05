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

    #[error("This token is already added for {side}")]
    DuplicateToken { side: String },

    #[error("This token is not in the registry")]
    TokenDoesNotExist {},

    #[error("This token is already disabled")]
    TokenAlreadyDisabled {},

    #[error("This token is already enabled")]
    TokenAlreadyEnabled {},
}
