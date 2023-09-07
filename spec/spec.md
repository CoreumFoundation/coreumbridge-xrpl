# XRPL two-way bridge spec.

The spec describes the technical solution of the XRPL two-way bridge.

## Technical solution

![High Level Architecture](./img/hl-arch.png)

### XRPL multi-signing account

The account holds the tokens issued on the XRPL on its balance. Depending on workflow it either uses the received tokens
balance to send to XRPL accounts (in case the account is not an issuer) or mints and sends the tokens to XRPL accounts (
in case it is the coreum token representation issued by the address). The account uses the multi-signing and public keys
associated with each relayer from the contract for the transactions signing.

### Bridge contract

The bridge contract is the major state transition of the bridge. It holds the operations state and protects the
execution using the trusted addresses voting/signing mechanisms. Also, it has an admin account that can change the
settings of the bridge.

#### Tokens registry

Before the bridging, a token (XRPL or coruem) should be manually registered for the bridging. The tokes that are not
registered can't be bridged.

##### XRPL native tokens registration

All tokens issued on XRPL that can be bridged from the XRPL to the coreum and back must have a representation on the
coreum. Such tokens should be registered by admin on the contract side with the XRPL issuer, XRPL currency, and fees.
The token's denom/subunit should be built uniquely using the XRPL issuer and XRPL currency. The decimals
are the same for all such tokens. Required features are minting, burning, and IBC.
During the registration, the contract issues a token and will be responsible for its minting later. After the
registration, the contract triggers the `submit-trust-set-for-xrpl-token` operation to allow the multi-signing account
to receive that token.
Check [register-token workflow](#register-token) for more details.

##### Coreum native tokens registration

All tokens issued on the coreum that can be bridged from the coreum to XRPL and back must have a representation on the
XRPL, managed by the multi-signing account. Such tokens should be registered by admin on the contract side with the
coreum denom, decimals, XRPL currency and fees.
Check [workflow](#register-token) for more details.

##### Token enabling/disabling

Any token can be disabled and enabled at any time by that admin. Any operation with the disabled token is prohibited by
the contract.

#### Operation queues

##### Evidence queue

The evidence queue is a queue that contains bridge operations which should be confirmed before execution. Each operation
has a type, associated ID (unique identifier/hash of the operation data in the scope of type), and a list of trusted
relayer addresses that provide the evidence. Once the contract receives enough evidences it removes the operation from
the queue and passes its data to the next step of a workflow.

##### Signing queue

The signing queue is a queue that contains bridge operations that should be signed before sending to XRPL and later
sent. Each operation has a type, associated ID (unique identifier/hash of the operation data in the scope of type), and
a list of signatures. Each relayer picks such operation, signs it and provides the signature for it. The operation keeps
receiving signatures and shares them with other relayers (using the contract). Each relayer validates the provided
signatures, filters valid, and checks whether it's possible to submit the transaction. If it is possible it builds the
transaction with valid signatures (the operation ID is in the memo) and submits the transaction to the XRPL. If multiple
relayers execute the same transaction and at the same time they receive a specific error which is an indicator for them,
go to the next item in the queue.
An additional sub-process of each relayer observes all multi-signing account transactions and once it reaches that
submitted transaction (matched by the ID in memo) it provides evidence with transaction status and data (using the
evidence queue). Once the
such evidence is confirmed, the tx result and data will be passed to the next step of a workflow.

##### Operations deduplication

The contract queues always have an operation ID which is built from the operation data. We do it to make the processing
idempotent. And let some operations be safely re-processed at any time.

#### Ticket allocation

##### Ticket allocation process

The XRPL tickets allow us to execute a transaction with non-sequential sequence numbers, hence we can execute multiple
transactions in parallel. When any workflow allocates the ticket and the free ticket length is less than max allowed the
contract triggers the `submit-increase-tickets` operation to increase the amount. Once the operation is confirmed, the
contract increases the free slots on the contract as well (based on the tx result).
Check [workflow](#allocate-ticket) for more details.

##### Manual ticket allocation

The admin can update free slots for the tickets manually by calling the contract. That process is a backup process for
the initial version of the bridge, for the cases of XRPL errors during the ticket allocation.

#### Tokens sending

##### Sending of tokens from XRPL

The contract receives the `send-to-coreum` request and starts the corresponding [workflow](#send-from-xrpl-to-coreum).

##### Sending of tokens to XRPL

The multis-signing account receives coins for a user, a relayer observes the transaction and initiates
the  [workflow](#send-from-coreum-to-xrpl).

##### Fees

###### Fee changing

Each token in the registry contains the fee config which consists of a bridging fee and a network fee. The bridging fee
is the fee that relayers earn for the transaction relaying. That fee covers their costs and provides some profit on top.
The network fee is the fee for the tokens which charge the commission for the transfer. That fee will be used to send
the locked tokens back in case they are locked either on the contract or on the multi-signing address. Both fees will be
taken from the amount a user sends.
The bridging fees are distributed across the relayer addresses
after the successful execution of the sending, and locked until a relayer manually requests it. After such a request the
accumulated bridging fee will be distributed equally to the current relayer addresses.

###### Fee re-config

The admin can change the token fees config at any time. Mostly it's required for the bridging fee since the price of the
token might be changed during the time, so the fee should be changed accordingly.

#### Keys rotation

All accounts that can interact with the contract or multi-signing account are registered on the contract. And can be
rotated using the key rotation workflow. The workflow is triggered by the admin. The admin provides
the new relayer coreum addresses, XRPL public keys and signing/evidence threshold.

Check [workflow](#rotate-keys) for more details.

### Relayer

The relayer is a connector of the multi-signing account on XRPL chain and smart contract. There are multiple instances
of relayers, one for each key pair in the smart contract and multi-signing account. Most of the workflows are
implemented as event processing produced by the contract and multi-signing account.

## Workflows

<!-- Source: https://drive.google.com/file/d/1wo-tO72N9Iww-iASw0DEk3NgA9-XQ93g/view -->

### Send from XRPL to coreum

![Send from XRPL to coreum](./img/send-from-XRPL-to-coreum.png)

### Send from coreum to XRPL

![Send from coreum to XRPL](./img/send-from-coreum-to-XRPL.png)

### Register token

![Register token](./img/register-token.png)

### Allocate-ticket

![Allocate-ticket](./img/allocate-ticket.png)

### Submit XRPL transaction

![Submission XRPL transaction](./img/submit-xrpl-tx.png)

### Rotate keys

![Rotate keys](./img/rotate-keys.png)


