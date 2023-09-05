use cosmwasm_schema::cw_serde;
use cosmwasm_std::Addr;
use cw_ownable::cw_ownable_execute;

use crate::state::Token;

#[cw_serde]
pub struct InstantiateMsg {
    //Addresses allowed to relay messages
    pub relayers: Vec<Addr>,
    //How many relayers need to sign the message.
    pub threshold: u32,
    //If ticket count goes under this amount, contract will ask for more tickets.
    pub min_tickets: u32,
    //Initial tickets reserved
    pub initial_tickets_from: u64,
    pub initial_tickets_to: u64,
    //Initial tokens to be added
    pub initial_xrp_tokens: Option<Vec<Token>>,
    pub initial_coreum_tokens: Option<Vec<Token>>,
}

#[cw_serde]
pub enum Side {
    Coreum,
    Xrpl,
}

impl Side {
    pub fn to_string(self) -> String {
        match self {
            Side::Coreum => "Coreum".to_string(),
            Side::Xrpl => "XRPL".to_string(),
        }
    }
}

#[cw_ownable_execute]
#[cw_serde]
pub enum ExecuteMsg {
    EnableToken { denom: String, side: Side },
    DisableToken { denom: String, side: Side },
}
