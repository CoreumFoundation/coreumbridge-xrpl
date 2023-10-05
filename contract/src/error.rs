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
    InvalidAmount {},

    #[error("InvalidMaxAllowedUsedTickets: Max allowed used tickets must be more than 1 and less or equal than 250")]
    InvalidMaxAllowedUsedTickets {},

    #[error("LastTicketReserved: Last available ticket is reserved for updating tickets")]
    LastTicketReserved {},

    #[error(
        "PendingTicketUpdate: There is a pending ticket update operation already in the queue"
    )]
    PendingTicketUpdate {},

    #[error("InvalidTicketAllocationEvidence: There must be tickets and a sequence number or ticket number")]
    InvalidTicketAllocationEvidence {},

    #[error("PendingOperationNotFound: There is no pending operation with this ticket/sequence number")]
    PendingOperationNotFound {},

    #[error("SignatureAlreadyProvided: There is already a signature provided for this relayer and this operation")]
    SignatureAlreadyProvided {},
}
