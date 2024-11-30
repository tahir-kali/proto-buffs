CREATE SEQUENCE IdSequence OPTIONS (sequence_kind = 'bit_reversed_positive');
DROP TABLE IF EXISTS CircleOfTrustUsers;
DROP TABLE IF EXISTS CircleOfTrust;
DROP TABLE IF EXISTS CircleOfTrustMembers;

CREATE TABLE CircleOfTrustUsers (
    UserId INT64 DEFAULT (GET_NEXT_SEQUENCE_VALUE(SEQUENCE IdSequence)),
    UserName STRING(256),
)PRIMARY KEY (UserId);

CREATE TABLE CircleOfTrust (
    CircleOfTrustId INT64 DEFAULT (GET_NEXT_SEQUENCE_VALUE(SEQUENCE IdSequence)),
    OwnerId INT64 NOT NULL,
    CircleOfTrustName STRING(256),
)PRIMARY KEY (CircleOfTrustId, OwnerId);

CREATE TABLE CircleOfTrustMembers (
    CircleOfTrustId INT64 DEFAULT (GET_NEXT_SEQUENCE_VALUE(SEQUENCE IdSequence)),
    Members BYTES(MAX), -- Store encoded protobuff data here 
    FOREIGN KEY (CircleOfTrustId) REFERENCES CircleOfTrust (CircleOfTrustId),
)PRIMARY KEY (CircleOfTrustId);
