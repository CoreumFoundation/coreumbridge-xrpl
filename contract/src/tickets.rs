use std::collections::VecDeque;

use cosmwasm_std::DepsMut;

use crate::{
    contract::check_operation_exists,
    error::ContractError,
    state::{
        Operation, OperationType, AVAILABLE_TICKETS, CONFIG, PENDING_OPERATIONS,
        PENDING_TICKET_UPDATE, USED_TICKETS,
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

    if used_tickets + 1 >= config.max_allowed_used_tickets
        && !PENDING_TICKET_UPDATE.load(deps.storage)?
    {
        let mut available_tickets = AVAILABLE_TICKETS.load(deps.storage)?;
        let ticket_to_update = available_tickets.pop_front().unwrap();
        AVAILABLE_TICKETS.save(deps.storage, &available_tickets)?;

        PENDING_OPERATIONS.save(
            deps.storage,
            ticket_to_update,
            &Operation {
                ticket_number: Some(ticket_to_update),
                sequence_number: None,
                operation_type: OperationType::AllocateTickets,
            },
        )?;
        PENDING_TICKET_UPDATE.save(deps.storage, &true)?;
    }

    USED_TICKETS.save(deps.storage, &(used_tickets + 1))?;

    Ok(())
}

pub fn handle_allocation_confirmation(
    deps: DepsMut,
    sequence_number: Option<u64>,
    ticket_number: Option<u64>,
    tickets: Option<Vec<u64>>,
    confirmed: bool,
) -> Result<(), ContractError> {
    let sequence_or_ticket_number =
        check_operation_exists(deps.as_ref(), sequence_number, ticket_number)?;
    //Remove the operation from the pending queue
    PENDING_OPERATIONS.remove(deps.storage, sequence_or_ticket_number);
    PENDING_TICKET_UPDATE.save(deps.storage, &false)?;

    //Allocate ticket numbers in our ticket array if operation is confirmed
    if confirmed {
        let mut available_tickets = AVAILABLE_TICKETS.load(deps.storage)?;

        let mut new_tickets = available_tickets.make_contiguous().to_vec();
        new_tickets.append(tickets.unwrap().as_mut());

        AVAILABLE_TICKETS.save(deps.storage, &VecDeque::from(new_tickets))?;

        USED_TICKETS.save(deps.storage, &0)?;
    }

    Ok(())
}
