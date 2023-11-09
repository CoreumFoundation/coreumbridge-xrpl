use std::collections::VecDeque;

use cosmwasm_std::{StdResult, Storage};

use crate::{
    error::ContractError,
    evidence::TransactionResult,
    operation::{Operation, OperationType},
    state::{
        AVAILABLE_TICKETS, CONFIG, PENDING_OPERATIONS, PENDING_TICKET_UPDATE, USED_TICKETS_COUNTER,
    },
};

// This function will be used to provide a ticket for a pending operation
pub fn allocate_ticket(storage: &mut dyn Storage) -> Result<u64, ContractError> {
    let available_tickets = AVAILABLE_TICKETS.load(storage)?;

    if available_tickets.is_empty() {
        return Err(ContractError::NoAvailableTickets {});
    }

    // This last ticket will always be reserved for an update of the tickets
    if available_tickets.len() <= 1 {
        return Err(ContractError::LastTicketReserved {});
    }

    let ticket = reserve_ticket(storage)?;

    Ok(ticket)
}

// Once we confirm/reject a transaction, we need to register a ticket as used
pub fn register_used_ticket(storage: &mut dyn Storage) -> Result<bool, ContractError> {
    let used_tickets = USED_TICKETS_COUNTER.load(storage)?;
    let config = CONFIG.load(storage)?;

    USED_TICKETS_COUNTER.save(storage, &(used_tickets + 1))?;

    // If we reach the max allowed tickets to be used, we need to create an operation to allocate new ones
    if used_tickets + 1 >= config.used_ticket_sequence_threshold
        && !PENDING_TICKET_UPDATE.load(storage)?
    {
        match reserve_ticket(storage) {
            Ok(ticket_to_update) => {
                PENDING_OPERATIONS.save(
                    storage,
                    ticket_to_update,
                    &Operation {
                        ticket_sequence: Some(ticket_to_update),
                        account_sequence: None,
                        signatures: vec![],
                        operation_type: OperationType::AllocateTickets {
                            number: config.used_ticket_sequence_threshold,
                        },
                    },
                )?;
                PENDING_TICKET_UPDATE.save(storage, &true)?
            }
            Err(ContractError::NoAvailableTickets {}) => return Ok(true),
            Err(e) => return Err(e),
        }
    }
    Ok(false)
}

pub fn handle_ticket_allocation_confirmation(
    storage: &mut dyn Storage,
    tickets: Option<Vec<u64>>,
    transaction_result: TransactionResult,
) -> Result<(), ContractError> {
    // We set pending update ticket to false because we complete the ticket allocation operation
    PENDING_TICKET_UPDATE.save(storage, &false)?;

    // Allocate ticket numbers in our ticket array if operation is accepted
    if transaction_result.eq(&TransactionResult::Accepted) {
        let mut available_tickets = AVAILABLE_TICKETS.load(storage)?;

        let mut new_tickets = available_tickets.make_contiguous().to_vec();
        new_tickets.append(tickets.clone().unwrap().as_mut());

        AVAILABLE_TICKETS.save(storage, &VecDeque::from(new_tickets))?;

        // Used tickets can't be under 0 if admin allocated more tickets than used tickets
        USED_TICKETS_COUNTER.update(storage, |used_tickets| -> StdResult<_> {
            Ok(used_tickets.saturating_sub(tickets.unwrap().len() as u32))
        })?;
    }

    Ok(())
}

// Extract a ticket from the available tickets
fn reserve_ticket(storage: &mut dyn Storage) -> Result<u64, ContractError> {
    let mut available_tickets = AVAILABLE_TICKETS.load(storage)?;
    if available_tickets.is_empty() {
        return Err(ContractError::NoAvailableTickets {});
    }

    let ticket_to_update = available_tickets.pop_front().unwrap();
    AVAILABLE_TICKETS.save(storage, &available_tickets)?;
    Ok(ticket_to_update)
}
