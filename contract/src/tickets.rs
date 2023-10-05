use std::collections::VecDeque;

use cosmwasm_std::{DepsMut, StdResult, Storage};

use crate::{
    error::ContractError,
    state::{
        remove_pending_operation, Operation, OperationType, AVAILABLE_TICKETS, CONFIG,
        PENDING_OPERATIONS, PENDING_TICKET_UPDATE, USED_TICKETS,
    },
};

pub fn _allocate_ticket(deps: DepsMut) -> Result<u64, ContractError> {
    let mut available_tickets = AVAILABLE_TICKETS.load(deps.storage)?;

    if available_tickets.len() < 2 {
        return Err(ContractError::LastTicketReserved {});
    }

    let ticket = available_tickets.pop_front().unwrap();

    AVAILABLE_TICKETS.save(deps.storage, &available_tickets)?;

    Ok(ticket)
}

pub fn _register_used_ticket(deps: DepsMut) -> Result<(), ContractError> {
    let used_tickets = USED_TICKETS.load(deps.storage)?;
    let config = CONFIG.load(deps.storage)?;

    USED_TICKETS.save(deps.storage, &(used_tickets + 1))?;

    if used_tickets + 1 >= config.max_allowed_used_tickets
        && !PENDING_TICKET_UPDATE.load(deps.storage)?
    {
        let ticket_to_update = get_ticket(deps.storage)?;

        PENDING_OPERATIONS.save(
            deps.storage,
            ticket_to_update,
            &Operation {
                ticket_number: Some(ticket_to_update),
                sequence_number: None,
                operation_type: OperationType::AllocateTickets {
                    number: config.max_allowed_used_tickets,
                },
            },
        )?;
        PENDING_TICKET_UPDATE.save(deps.storage, &true)?;
    }

    Ok(())
}

pub fn handle_allocation_confirmation(
    storage: &mut dyn Storage,
    sequence_or_ticket_number: u64,
    tickets: Option<Vec<u64>>,
    confirmed: bool,
) -> Result<(), ContractError> {
    //Remove the operation from the pending queue
    remove_pending_operation(storage, sequence_or_ticket_number)?;

    //Allocate ticket numbers in our ticket array if operation is confirmed
    if confirmed {
        let mut available_tickets = AVAILABLE_TICKETS.load(storage)?;

        let mut new_tickets = available_tickets.make_contiguous().to_vec();
        new_tickets.append(tickets.clone().unwrap().as_mut());

        AVAILABLE_TICKETS.save(storage, &VecDeque::from(new_tickets))?;

        USED_TICKETS.update(storage, |used_tickets| -> StdResult<_> {
            let new_used_tickets = used_tickets
                .checked_sub(tickets.unwrap().len() as u32)
                .unwrap_or_default();
            Ok(new_used_tickets)
        })?;
    }

    Ok(())
}

fn get_ticket(storage: &mut dyn Storage) -> Result<u64, ContractError> {
    let mut available_tickets = AVAILABLE_TICKETS.load(storage)?;
    let ticket_to_update = available_tickets.pop_front().unwrap();
    AVAILABLE_TICKETS.save(storage, &available_tickets)?;
    Ok(ticket_to_update)
}
