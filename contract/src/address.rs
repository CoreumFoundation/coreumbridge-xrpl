use crate::error::ContractError;
use bs58::Alphabet;
use sha2::{Digest, Sha256};

pub fn validate_xrpl_address(address: String) -> Result<(), ContractError> {
    let data = bs58::decode(&address)
        .with_alphabet(Alphabet::RIPPLE)
        .into_vec()
        .map_err(|_| ContractError::InvalidXRPLAddress {
            address: address.to_owned(),
        })?;

    if data.len() != 25 || data[0] != 0 {
        return Err(ContractError::InvalidXRPLAddress { address });
    }

    // Check if the payload produces the provided checksum
    let expected_checksum = &checksum(&data[..21])[..4];
    let provided_checksum = &data[21..];

    if *expected_checksum != *provided_checksum {
        return Err(ContractError::InvalidXRPLAddress { address });
    }

    Ok(())
}

pub fn checksum(data: &[u8]) -> Vec<u8> {
    Sha256::digest(Sha256::digest(data)).to_vec()
}
