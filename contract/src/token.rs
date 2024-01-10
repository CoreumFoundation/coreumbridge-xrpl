use crate::{
    contract::{validate_sending_precision, XRP_CURRENCY, XRP_ISSUER},
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
pub fn set_token_state(
    state: &mut TokenState,
    target_state: Option<TokenState>,
) -> Result<(), ContractError> {
    if let Some(target_state) = target_state {
        if (*state).eq(&TokenState::Inactive) || (*state).eq(&TokenState::Processing) {
            return Err(ContractError::TokenStateIsImmutable {});
        }
        if target_state.eq(&TokenState::Inactive) || target_state.eq(&TokenState::Processing) {
            return Err(ContractError::InvalidTargetTokenState {});
        }

        *state = target_state;
    }

    Ok(())
}

// Helper function to update the sending precision of a token
pub fn set_token_sending_precision(
    sending_precision: &mut i32,
    target_sending_precision: Option<i32>,
    decimals: u32,
) -> Result<(), ContractError> {
    if let Some(target_sending_precision) = target_sending_precision {
        validate_sending_precision(target_sending_precision, decimals)?;

        *sending_precision = target_sending_precision;
    }

    Ok(())
}
