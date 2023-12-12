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
pub fn update_token_status(
    status: &mut TokenState,
    new_status: Option<TokenState>,
) -> Result<(), ContractError> {
    if let Some(new_status) = new_status {
        if (*status).eq(&TokenState::Inactive) || (*status).eq(&TokenState::Processing) {
            return Err(ContractError::TokenStatusNotUpdatable {});
        }
        if new_status.eq(&TokenState::Inactive) || new_status.eq(&TokenState::Processing) {
            return Err(ContractError::InvalidTokenStatusUpdate {});
        }

        *status = new_status;
    }

    Ok(())
}
