use crate::error::ContractError;
use base58::FromBase58;
use sha2::{Digest, Sha256};

/// Utility for mapping the ripple base58 alphabet to bitcoin base58 alphabet
static XRP_2_BTC_BS58_MAP: [i8; 128] = [
    -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
    -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
    -1, 115, 97, 56, 78, 105, 104, 85, 110, 57, -1, -1, -1, -1, -1, -1, -1, 119, 66, 102, 68, 70,
    112, 71, 72, -1, 74, 75, 76, 77, 69, -1, 80, 81, 82, 83, 84, 67, 86, 87, 88, 89, 90, -1, -1,
    -1, -1, -1, -1, 54, 98, 99, 100, 101, 55, 103, 52, 114, 106, 107, -1, 109, 53, 111, 50, 113,
    49, 51, 116, 117, 118, 65, 120, 121, 122, -1, -1, -1, -1, -1,
];

pub fn validate_xrpl_address(address: String) -> Result<(), ContractError> {
    let s = to_btc_bs58(&address)?;
    let data = s
        .from_base58()
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

fn to_btc_bs58(s: &str) -> Result<String, ContractError> {
    let to: Vec<u8> = s
        .as_bytes()
        .iter()
        .map(|b| XRP_2_BTC_BS58_MAP[*b as usize] as u8)
        .collect();

    String::from_utf8(to).map_err(|_| ContractError::InvalidXRPLAddress {
        address: s.to_string(),
    })
}

pub fn checksum(data: &[u8]) -> Vec<u8> {
    Sha256::digest(Sha256::digest(data)).to_vec()
}
