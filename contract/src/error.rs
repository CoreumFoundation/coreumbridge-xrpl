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

    #[error("InvalidThreshold: Threshold can not be higher than amount of relayers")]
    InvalidThreshold {},

    #[error("CoreumTokenAlreadyRegistered: Token {} already registered", denom)]
    CoreumTokenAlreadyRegistered { denom: String },
  
    #[error(
        "XRPLTokenAlreadyRegistered: Token with issuer: {} and currency: {} is already registered",
        issuer,
        currency
    )]
    XRPLTokenAlreadyRegistered { issuer: String, currency: String },

    #[error("InvalidIssueFee: Need to send exactly the issue fee amount")]
    InvalidIssueFee {},

    #[error(
        "RegistrationFailure: Random currency/denom generated already exists, please try again"
    )]
    RegistrationFailure {},

    #[error("UnauthorizedSender: Sender is not a valid relayer")]
    UnauthorizedSender {},

    #[error("TokenNotRegistered: The token must be registered first before bridging")]
    TokenNotRegistered {},

    #[error("OperationAlreadyExecuted: The operation has already been executed")]
    OperationAlreadyExecuted {},

    #[error(
        "EvidenceAlreadyProvided: The relayer already provided its evidence for the operation"
    )]
    EvidenceAlreadyProvided {},

    #[error("InvalidAmount: Amount must be more than 0")]
    InvalidAmount {}
}
