use cosmwasm_std::StdError;
use cw_ownable::OwnershipError;
use cw_utils::PaymentError;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error(transparent)]
    Ownership(#[from] OwnershipError),

    #[error("Payment error: {0}")]
    Payment(#[from] PaymentError),

    #[error("Threshold can not be higher than amount of relayers")]
    InvalidThreshold {},

    #[error("Token {} already registered", denom)]
    CoreumTokenAlreadyRegistered { denom: String },

    #[error("Need to send exactly the issue fee amount")]
    InvalidIssueFee {},

    #[error("Random XRPL currency generated already exists, please try again")]
    RegistrationFailure {},
}
