/**
 * Returns the status of Salvo Group Validity Flag
 *
 * @export
 * @enum {number}
 */
export enum ValidityFlag {
    /**
     * Valid connect index returned, more data available
     */
    VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE = 0x00,

    /**
     * Valid connect index returned, last in queue
     */
    VALIDITY_CONNECT_INDEX_RETURNED_LAST_IN_QUEUE = 0x01,

    /**
     * Invalid connect (no data in SALVO)
     */
    INVALID_CONNECT_NO_DATA_IN_SALVO = 0x02
}

/**
 * CrossPoint Go Group Salvo Message CommandOptions
 * + Valid connect index returned, more data available
 * + Valid connect index returned, last in queue
 * + Invalid connect (no data in SALVO)

 * @export
 * @interface CrossPointGroupSalvoTallyCommandOptions
 */
export interface CrossPointGroupSalvoTallyCommandOptions {
    /**
     * Validity flag
     *
     * @type {number}
     * @memberof CrossPointGroupSalvoTallyCommandOptions
     */
    salvoValidityFlag: ValidityFlag;
}

/**
 * Utility class providing some extra functionality on the CrosspointGroupSalvoTallyMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {CrossPointGroupSalvoTallyCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: CrossPointGroupSalvoTallyCommandOptions): string {
        return `salvoValidityFlag:${this.toStringSalvoCrossPointStatus(options.salvoValidityFlag)}`;
    }

    /**
     * Gets a textual representation of the CrosspointGroupSalvoTallyMessage
     *
     * @static
     * @param {ProtectDetails} data the salvoValidityFlag
     * @returns {string} the salvoValidityFlag textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringSalvoCrossPointStatus(data: ValidityFlag): string {
        return `( ${data} - ${ValidityFlag[data]} )`;
    }
}
