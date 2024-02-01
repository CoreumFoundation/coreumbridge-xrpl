use cosmwasm_std::{DivideByZeroError, OverflowError, StdError, Uint128};
use cw_ownable::OwnershipError;
use cw_utils::PaymentError;
use thiserror::Error;

use crate::contract::{MAX_RELAYERS, MAX_TICKETS};

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

    #[error("InvalidXRPLAddress: XRPL address {} is not valid", address)]
    InvalidXRPLAddress { address: String },

    #[error("DuplicatedRelayerXRPLAddress: All relayers must have different XRPL addresses")]
    DuplicatedRelayerXRPLAddress {},

    #[error("DuplicatedRelayerXRPLPubKey: All relayers must have different XRPL public keys")]
    DuplicatedRelayerXRPLPubKey {},

    #[error("DuplicatedRelayerCoreumAddress: All relayers must have different coreum addresses")]
    DuplicatedRelayerCoreumAddress {},

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

    #[error("InvalidUsedTicketSequenceThreshold: Used ticket sequences threshold must be more than 1 and less or equal than {}", MAX_TICKETS)]
    InvalidUsedTicketSequenceThreshold {},

    #[error("NoAvailableTickets: There are no available tickets")]
    NoAvailableTickets {},

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

    #[error("InvalidSuccessfulTransactionResultEvidence: An evidence with a successful transaction must contain a transaction hash")]
    InvalidSuccessfulTransactionResultEvidence {},

    #[error("InvalidFailedTransactionResultEvidence: An evidence with an failed transaction can't have a transaction hash")]
    InvalidFailedTransactionResultEvidence {},

    #[error("InvalidTicketAllocationEvidence: Tickets have to be present if operation is accepted and absent if operation is rejected or invalid")]
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

    #[error("InvalidTicketSequenceToAllocate: The number of tickets to recover must be greater than used ticket threshold and less than or equal to max allowed")]
    InvalidTicketSequenceToAllocate {},

    #[error("InvalidXRPLIssuer: The issuer must be a valid XRPL address")]
    InvalidXRPLIssuer {},

    #[error("InvalidXRPLCurrency: The currency must be a valid XRPL currency")]
    InvalidXRPLCurrency {},

    #[error("TokenNotEnabled: This token must be enabled to be bridged")]
    TokenNotEnabled {},

    #[error("XRPLTokenNotInProcessing: This token must be in processing state to be enabled")]
    XRPLTokenNotInProcessing {},

    #[error("XRPLTokenNotInactive: To recover this token it must be inactive")]
    XRPLTokenNotInactive {},

    #[error("AmountSentIsZeroAfterTruncation: Amount sent is zero after truncating to sending precision")]
    AmountSentIsZeroAfterTruncation {},

    #[error("MaximumBridgedAmountReached: The maximum amount this contract can have bridged has been reached")]
    MaximumBridgedAmountReached {},

    #[error(
    "InvalidSendingPrecision: The sending precision can't be more than the token decimals or less than the negative token decimals"
    )]
    InvalidSendingPrecision {},

    #[error(
        "TokenSendingPrecisionTooHigh: The sending precision can't be more than the token decimals"
    )]
    TokenSendingPrecisionTooHigh {},

    #[error("InvalidOperationResult: OperationResult doesn't match a Pending Operation with the right Operation Type")]
    InvalidOperationResult {},

    #[error("CannotCoverBridgingFees: The amount sent is not enough to cover the bridging fees")]
    CannotCoverBridgingFees {},

    #[error("TokenStateIsImmutable: Current token state is immutable")]
    TokenStateIsImmutable {},

    #[error("InvalidTargetTokenState: A token state can only be updated to enabled or disabled")]
    InvalidTargetTokenState {},

    #[error("InvalidTargetMaxHoldingAmount: Max holding amount can't be less than the current amount of tokens held in the bridge")]
    InvalidTargetMaxHoldingAmount {},

    #[error(
        "PendingRefundNotFound: There is no pending refund for this user and pending operation id"
    )]
    PendingRefundNotFound {},

    #[error(
        "NotEnoughFeesToClaim: The fee {} {} is not claimable because there are not enough fees collected",
        amount,
        denom
    )]
    NotEnoughFeesToClaim { denom: String, amount: Uint128 },

    #[error("NotOwnerOrRelayer: The sender is not the owner of the contract or a relayer. Unauthorized.")]
    NotOwnerOrRelayer {},

    #[error("ThresholdZero: Evidence threshold can't be 0")]
    ThresholdZero {},

    #[error(
        "TooManyRelayers: too many relayers provided, max allowed is {}",
        MAX_RELAYERS
    )]
    TooManyRelayers {},

    #[error("BridgeHalted: The bridge is currently halted and this operation is not authorized")]
    BridgeHalted {},

    #[error("RotateKeysOngoing: Can't perform this operation while there is a rotate key operation ongoing")]
    RotateKeysOngoing {},

    #[error(
        "OperationVersionMismatch: Can't add a signature for an operation with a different version"
    )]
    OperationVersionMismatch {},

    #[error("ProhibitedRecipient: The recipient cannot be the bridge")]
    ProhibitedRecipient {},

    #[error("DeliverAmountIsProhibited: Optional deliver_amount field is only used for XRPL originated tokens (except XRP) being bridged back")]
    DeliverAmountIsProhibited {},

    #[error(
        "InvalidDeliverAmount: Field deliver_amount can't be greater than funds attached minus fees"
    )]
    InvalidDeliverAmount {},

    #[error("InvalidSignatureLength: The signature sent can't be longer than 200 characters")]
    InvalidSignatureLength {},

    #[error(
        "InvalidXRPLAmount: Amounts sent to XRPL can't have more than 17 digits after trimming trailing zeroes"
    )]
    InvalidXRPLAmount {},
}
