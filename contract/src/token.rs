use crate::{
    contract::{XRP_CURRENCY, XRP_ISSUER},
    error::ContractError,
    state::TokenState,
};

// Build the key to access the Tokens saved in state
pub fn build_xrpl_token_key(issuer: String, currency: String) -> String {
    // Issuer+currency is the key we use to find an XRPL
    let mut key = issuer;
    key.push_str(currency.as_str());
    key
}

// Helper to distinguish between the XRP token and other XRPL originated tokens
pub fn is_token_xrp(issuer: String, currency: String) -> bool {
    issuer == XRP_ISSUER && currency == XRP_CURRENCY
}

// Helper function to update the status of a token
pub fn update_token_state(
    state: &mut TokenState,
    new_state: Option<TokenState>,
) -> Result<(), ContractError> {
    if let Some(new_state) = new_state {
        if (*state).eq(&TokenState::Inactive) || (*state).eq(&TokenState::Processing) {
            return Err(ContractError::TokenStateNotUpdatable {});
        }
        if new_state.eq(&TokenState::Inactive) || new_state.eq(&TokenState::Processing) {
            return Err(ContractError::InvalidTokenStateUpdate {});
        }

        *state = new_state;
    }

    Ok(())
}
