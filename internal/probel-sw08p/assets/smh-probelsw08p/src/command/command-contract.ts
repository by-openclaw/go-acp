import { LocaleData } from '../common/locale-data/locale-data.model';

export abstract class CommandSymbol {
    // http://www.asciitable.com/
    static readonly DLE = 0x10; // Data Layer Enrichment (DLE) character.
    static readonly STX = 0x02; // STart of Text (STX) character
    static readonly ETX = 0x03; // End-of Text (EOT) character
    static readonly ACK = 0x06; // Acknowledge message (DLE, ACK) 2 bytes.
    static readonly NACK = 0x15; // No acknowledge message (DLE, NAK) 2 bytes
    static readonly CR = 0x0d; // Carriage return = \n
    static readonly LF = 0x0a; // New Line - Line Feed
}

export abstract class CommandPacket {
    static readonly SOM = new Buffer([CommandSymbol.DLE, CommandSymbol.STX]);
    static readonly EOM = new Buffer([CommandSymbol.DLE, CommandSymbol.ETX]);
    static readonly ACK = new Buffer([CommandSymbol.DLE, CommandSymbol.ACK]);
    static readonly NACK = new Buffer([CommandSymbol.DLE, CommandSymbol.NACK]);
}

export type SOM = Buffer; // Start of message (DLE, STX) 2 bytes
export type DATA = Buffer; // Message data from the application layer
export type BTC = number; // Byte count for the data section
export type CHK = number; // Checksumm (8 bit, 2's compliment of DATA & BTC) 1 byte
export type EOM = Buffer; // End of message (DLE, ETX) 2 bytes

export interface CommandIdentifier {
    id: number;
    name: string;
    rxTxType: RxTxType;
    isExtended: boolean;
}

export type RxTxType = 'RX' | 'TX';

const buildCommandIdentifier = (
    id: number,
    name: string,
    rxTxType: RxTxType,
    isExtended = false
): CommandIdentifier => ({ id, name, rxTxType, isExtended });

const RX_GENERAL_COMMAND_IDENTIFIER = {
    CROSSPOINT_INTERROGATE_MESSAGE: buildCommandIdentifier(0x01, 'CROSSPOINT_INTERROGATE_MESSAGE', 'RX'),
    CROSSPOINT_CONNECT_MESSAGE: buildCommandIdentifier(0x02, 'CROSSPOINT_CONNECT_MESSAGE', 'RX'),
    MAINTENANCE_MESSAGE: buildCommandIdentifier(0x07, 'MAINTENANCE_MESSAGE', 'RX'),
    DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE: buildCommandIdentifier(
        0x08,
        'DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE',
        'RX'
    ),
    PROTECT_INTERROGATE_MESSAGE: buildCommandIdentifier(0x0a, 'PROTECT_INTERROGATE_MESSAGE', 'RX'),
    PROTECT_CONNECT_MESSAGE: buildCommandIdentifier(0x0c, 'PROTECT_CONNECT_MESSAGE', 'RX'),
    PROTECT_DIS_CONNECT_MESSAGE: buildCommandIdentifier(0x0e, 'PROTECT_DIS_CONNECT_MESSAGE', 'RX'),
    PROTECT_DEVICE_NAME_REQUEST_MESSAGE: buildCommandIdentifier(0x11, 'PROTECT_DEVICE_NAME_REQUEST_MESSAGE', 'RX'),
    PROTECT_TALLY_DUMP_REQUEST_MESSAGE: buildCommandIdentifier(0x13, 'PROTECT_TALLY_DUMP_REQUEST_MESSAGE', 'RX'),
    CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE: buildCommandIdentifier(0x15, 'CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE', 'RX'),
    MASTER_PROTECT_CONNECT_MESSAGE: buildCommandIdentifier(0x1d, 'MASTER_PROTECT_CONNECT_MESSAGE', 'RX'),
    ALL_SOURCE_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(0x64, 'ALL_SOURCE_NAMES_REQUEST_MESSAGE', 'RX'),
    SINGLE_SOURCE_NAME_REQUEST_MESSAGE: buildCommandIdentifier(0x65, 'SINGLE_SOURCE_NAME_REQUEST_MESSAGE', 'RX'),
    ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0x66,
        'ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX'
    ),
    SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0x67,
        'SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX'
    ),
    CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE: buildCommandIdentifier(
        0x70,
        'CROSSPOINT_TIE_LINE_INTERROGATE_MESSAGE',
        'RX'
    ),
    ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0x72,
        'ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX'
    ),
    SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0x73,
        'SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX'
    ),
    UPDATE_NAME_REQUEST_MESSAGE: buildCommandIdentifier(0x75, 'UPDATE_NAME_REQUEST_MESSAGE', 'RX'),
    CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE: buildCommandIdentifier(
        0x78,
        'CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE',
        'RX'
    ),
    CROSSPOINT_GO_GROUP_SALVO_MESSAGE: buildCommandIdentifier(0x79, 'CROSSPOINT_GO_GROUP_SALVO_MESSAGE', 'RX'),
    CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE: buildCommandIdentifier(0x7c, 'CROSSPOINT_GO_GROUP_SALVO_MESSAGE', 'RX')
};

const RX_EXTENDED_COMMAND_IDENTIFIER = {
    CROSSPOINT_INTERROGATE_MESSAGE: buildCommandIdentifier(0x81, 'CROSSPOINT_INTERROGATE_MESSAGE', 'RX', true),
    CROSSPOINT_CONNECT_MESSAGE: buildCommandIdentifier(0x82, 'CROSSPOINT_CONNECT_MESSAGE', 'RX', true),
    PROTECT_INTERROGATE_MESSAGE: buildCommandIdentifier(0x8a, 'PROTECT_INTERROGATE_MESSAGE', 'RX', true),
    PROTECT_CONNECT_MESSAGE: buildCommandIdentifier(0x8c, 'PROTECT_CONNECT_MESSAGE', 'RX', true),
    PROTECT_DIS_CONNECT_MESSAGE: buildCommandIdentifier(0x8e, 'PROTECT_DIS_CONNECT_MESSAGE', 'RX', true),
    PROTECT_TALLY_DUMP_REQUEST_MESSAGE: buildCommandIdentifier(0x93, 'PROTECT_TALLY_DUMP_REQUEST_MESSAGE', 'RX', true),
    CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE: buildCommandIdentifier(
        0x95,
        'CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE',
        'RX',
        true
    ),
    ALL_SOURCE_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(0xe4, 'ALL_SOURCE_NAMES_REQUEST_MESSAGE', 'RX', true),
    SINGLE_SOURCE_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0xe5,
        'SINGLE_SOURCE_NAMES_REQUEST_MESSAGE',
        'RX',
        true
    ),
    ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0xe6,
        'ALL_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX',
        true
    ),
    SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE: buildCommandIdentifier(
        0xe7,
        'SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE',
        'RX',
        true
    ),
    CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE: buildCommandIdentifier(
        0xf8,
        'CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE',
        'RX',
        true
    ),
    CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE: buildCommandIdentifier(
        0xfc,
        'CROSSPOINT_SALVO_GROUP_INTERROGATE_MESSAGE',
        'RX',
        true
    )
};

const TX_GENERAL_COMMAND_IDENTIFIER = {
    CROSSPOINT_TALLY_MESSAGE: buildCommandIdentifier(0x03, 'CROSSPOINT_TALLY_MESSAGE', 'TX'),
    CROSSPOINT_CONNECTED_MESSAGE: buildCommandIdentifier(0x04, 'CROSSPOINT_CONNECTED_MESSAGE', 'TX'),
    DUAL_CONTROLLER_STATUS_RESPONSE_MESSAGE: buildCommandIdentifier(
        0x09,
        'DUAL_CONTROLLER_STATUS_RESPONSE_MESSAGE',
        'TX'
    ),
    PROTECT_TALLY_MESSAGE: buildCommandIdentifier(0x0b, 'PROTECT_TALLY_MESSAGE', 'TX'),
    PROTECT_CONNECTED_MESSAGE: buildCommandIdentifier(0x0d, 'PROTECT_CONNECTED_MESSAGE', 'TX'),
    PROTECT_DIS_CONNECTED_MESSAGE: buildCommandIdentifier(0x0f, 'PROTECT_DIS_CONNECTED_MESSAGE', 'TX'),
    PROTECT_DEVICE_NAME_RESPONSE_MESSAGE: buildCommandIdentifier(0x12, 'PROTECT_DEVICE_NAME_RESPONSE_MESSAGE', 'TX'),
    PROTECT_TALLY_DUMP_MESSAGE: buildCommandIdentifier(0x14, 'PROTECT_TALLY_DUMP_MESSAGE', 'TX'),
    CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE: buildCommandIdentifier(0x16, 'CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE', 'TX'),
    CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE: buildCommandIdentifier(0x17, 'CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE', 'TX'),
    SOURCE_NAMES_RESPONSE_MESSAGE: buildCommandIdentifier(0x6a, 'SOURCE_NAMES_RESPONSE_MESSAGE', 'TX'),
    DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE: buildCommandIdentifier(
        0x6b,
        'DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE',
        'TX'
    ),
    CROSSPOINT_TIE_LINE_TALLY_MESSAGE: buildCommandIdentifier(0x71, 'CROSSPOINT_TIE_LINE_TALLY_MESSAGE', 'TX'),
    SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE: buildCommandIdentifier(
        0x74,
        'SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE',
        'TX'
    ),
    CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE: buildCommandIdentifier(
        0x7a,
        'CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE',
        'TX'
    ),
    CROSSPOINT_GO_DONE_GROUP_SALVO_ACKNOWLEDGE_MESSAGE: buildCommandIdentifier(
        0x7b,
        'CROSSPOINT_GO_DONE_GROUP_SALVO_ACKNOWLEDGE_MESSAGE',
        'TX'
    ),
    CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE: buildCommandIdentifier(0x7d, 'CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE', 'TX')
};

const TX_EXTENDED_COMMAND_IDENTIFIER = {
    CROSSPOINT_TALLY_MESSAGE: buildCommandIdentifier(0x83, 'CROSSPOINT_TALLY_MESSAGE', 'TX', true),
    CROSSPOINT_CONNECTED_MESSAGE: buildCommandIdentifier(0x84, 'CROSSPOINT_CONNECTED_MESSAGE', 'TX', true),
    PROTECT_TALLY_MESSAGE: buildCommandIdentifier(0x8b, 'PROTECT_TALLY_MESSAGE', 'TX', true),
    PROTECT_CONNECTED_MESSAGE: buildCommandIdentifier(0x8d, 'PROTECT_CONNECTED_MESSAGE', 'TX', true),
    PROTECT_DIS_CONNECTED_MESSAGE: buildCommandIdentifier(0x8f, 'PROTECT_DIS_CONNECTED_MESSAGE', 'TX', true),
    PROTECT_TALLY_DUMP_MESSAGE: buildCommandIdentifier(0x94, 'PROTECT_TALLY_DUMP_MESSAGE', 'TX', true),
    CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE: buildCommandIdentifier(
        0x97,
        'CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE',
        'TX',
        true
    ),
    SOURCE_NAMES_RESPONSE_MESSAGE: buildCommandIdentifier(0xea, 'SOURCE_NAMES_RESPONSE_MESSAGE', 'TX', true),
    DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE: buildCommandIdentifier(
        0xeb,
        'DESTINATION_ASSOCIATION_NAMES_RESPONSE_MESSAGE',
        'TX',
        true
    ),
    CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE: buildCommandIdentifier(
        0xfa,
        'CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE',
        'TX',
        true
    ),
    CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE: buildCommandIdentifier(
        0xfd,
        'CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE',
        'TX',
        true
    )
};

/**
 * Probel SW-P-08
 * List of Command Identifiers RX and TX
 * @export
 * @class CommandIdentifiers
 */
export class CommandIdentifiers {
    /**
     * As Remote Device -
     * General and Extended Commands Received
     *
     * E.G. SMH
     * @static
     * @memberof CommandIdentifiers
     */
    static readonly RX = {
        APP_KEEPALIVE_RESPONSE: buildCommandIdentifier(0x22, 'APP_KEEPALIVE_RESPONSE', 'RX'),
        GENERAL: RX_GENERAL_COMMAND_IDENTIFIER,
        EXTENDED: RX_EXTENDED_COMMAND_IDENTIFIER
    };
    /**
     * As Pro-Bel Controller -
     * General and Extended Commands Transmitted
     *
     * E.G. Matrix
     * @static
     * @memberof CommandIdentifiers
     */
    static readonly TX = {
        APP_KEEPALIVE_REQUEST: buildCommandIdentifier(0x11, 'APP_KEEPALIVE_REQUET', 'TX'),
        GENERAL: TX_GENERAL_COMMAND_IDENTIFIER,
        EXTENDED: TX_EXTENDED_COMMAND_IDENTIFIER
    };
}

export interface DisplayCommand {
    SOM: string;
    DATA: string;
    BTC: string;
    CHK: string;
    EOM: string;
}
/**
 * Defines the Validator contract mainly used for command options and parameters
 *
 * @export
 * @interface IValidator
 * @template TData the data to validate
 */
export interface IValidator<TData> {
    /**
     * The data to validator will validate
     *
     * @type {TData}
     * @memberof IValidator
     */
    readonly data: TData;

    /**
     * Validates the data and returns one error message for each invalid data properties
     *
     * @returns {Record<string, LocaleData>} localized errors list
     * @memberof IValidator
     */
    validate(): Record<string, LocaleData>;
}

/**
 *
 *
 * @export
 * @class ValidationError
 * @extends {Error}
 */
export class ValidationError extends Error {
    constructor(message: string, public validationErrors?: Record<string, LocaleData>) {
        super(message);
        this.name = ValidationError.name;
    }
}
