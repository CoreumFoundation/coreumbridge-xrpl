use cosmwasm_std::{DivideByZeroError, OverflowError, StdError};
use cw_ownable::OwnershipError;
use cw_utils::PaymentError;
use thiserror::Error;

use crate::contract::MAX_TICKETS;

#[derive(Error, Debug)]
pub enum ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error(transparent)]
    Ownership(#[from] OwnershipError),

    #[error(transparent)]
    OverflowError(#[from] OverflowError),

    #[error(transparent)]
    DivideByZeroError(#[from] DivideByZeroError),

    #[error("Payment error: {0}")]
    Payment(#[from] PaymentError),

    #[error("InvalidThreshold: Threshold can not be higher than amount of relayers")]
    InvalidThreshold {},

    #[error("InvalidXRPLAddress: XRPL address {} is not valid, must start with r and have a length between 24 and 34", address)]
    InvalidXRPLAddress { address: String },

    #[error("RepeatedRelayerXRPLAddress: All relayers must have different XRPL addresses")]
    RepeatedRelayerXRPLAddress {},

    #[error("RepeatedRelayerXRPLPubKey: All relayers must have different XRPL public keys")]
    RepeatedRelayerXRPLPubKey {},

    #[error("RepeatedRelayerCoreumAddress: All relayers must have different coreum addresses")]
    RepeatedRelayerCoreumAddress {},

    #[error("CoreumTokenAlreadyRegistered: Token {} already registered", denom)]
    CoreumTokenAlreadyRegistered { denom: String },

    #[error(
        "XRPLTokenAlreadyRegistered: Token with issuer: {} and currency: {} is already registered",
        issuer,
        currency
    )]
    XRPLTokenAlreadyRegistered { issuer: String, currency: String },

    #[error("InvalidFundsAmount: Need to send exactly the issue fee amount")]
    InvalidFundsAmount {},

    #[error("RegistrationFailure: Currency/denom generated already exists, please try again")]
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

    #[error("InvalidUsedTicketsThreshold: Used tickets threshold must be more than 1 and less or equal than {}", MAX_TICKETS)]
    InvalidUsedTicketsThreshold {},

    #[error("LastTicketReserved: Last available ticket is reserved for updating tickets")]
    LastTicketReserved {},

    #[error("StillHaveAvailableTickets: Can't recover tickets if we still have tickets available")]
    StillHaveAvailableTickets {},

    #[error(
        "PendingTicketUpdate: There is a pending ticket update operation already in the queue"
    )]
    PendingTicketUpdate {},

    #[error("InvalidTransactionResultEvidence: An evidence must contain only one of sequence numer or ticket number")]
    InvalidTransactionResultEvidence {},

    #[error("InvalidValidTransactionResultEvidence: An evidence with a valid transaction must contain a transaction hash")]
    InvalidValidTransactionResultEvidence {},

    #[error("InvalidNotValidTransactionResultEvidence: An evidence with an invalid transaction can't have a transaction hash or be confirmed")]
    InvalidNotValidTransactionResultEvidence {},

    #[error("InvalidTicketAllocationEvidence: There must be tickets if operation is confirmed and valid and there can't be tickets if operation is not confirmed or not valid")]
    InvalidTicketAllocationEvidence {},

    #[error(
        "PendingOperationNotFound: There is no pending operation with this ticket/sequence number"
    )]
    PendingOperationNotFound {},

    #[error(
        "PendingOperationAlreadyExists: There is already a pending operation with this operation id"
    )]
    PendingOperationAlreadyExists {},

    #[error("SignatureAlreadyProvided: There is already a signature provided for this relayer and this operation")]
    SignatureAlreadyProvided {},

    #[error("InvalidTicketNumberToAllocate: The number of tickets to recover must be greater than used ticket threshold and less than or equal to max allowed")]
    InvalidTicketNumberToAllocate {},

    #[error("InvalidXRPLIssuer: The issuer must be a valid XRPL address")]
    InvalidXRPLIssuer {},

    #[error("InvalidXRPLCurrency: The currency must be a valid XRPL currency")]
    InvalidXRPLCurrency {},

    #[error("AmountSentIsZeroAfterTruncation: Amount sent is zero after truncating to sending precision")]
    AmountSentIsZeroAfterTruncation {},

    #[error("MaximumBridgedAmountReached: The maximum amount this contract can have bridged has been reached")]
    MaximumBridgedAmountReached {},

    #[error(
        "InvalidSendingPrecision: The sending precision can't be more than the token decimals or less than the negative token decimals"
    )]
    InvalidSendingPrecision {},
}
