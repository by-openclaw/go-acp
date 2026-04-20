/**
 * Returns Probel SW-P-08 - Protect Tally Dump Message CMD_020_0X14
 *
 * Gets the index of the Protect Details
 *
 * @export
 * @enum {number}
 */
export enum ProtectDetails {
    /**
     * Protect STATUS
     */
    NOT_PROTECTED = 0x00,
    /**
     * Protect STATUS
     */
    PRO_BEL_PROTECTED = 0x01,
    /**
     * Protect STATUS
     */
    PRO_BEL_OVERRIDE_PROTECTED = 0x02,
    /**
     * Protect STATUS
     */
    OEM_PROTECTED = 0x03
}

/**
 * Returns the Protect Tally Dump Message Command Options
 *
 * @export
 * @interface ProtectTallyDumpCommandOptions
 */
export interface ProtectTallyDumpCommandOptions {
    /**
     * Protect Details
     *
     * @type {number}
     * @memberof ProtectTallyDumpCommandOptions
     */
    protectDetails: ProtectDetails;
}

/**
 * Utility class providing some extra functionality on the ProtectTallyDumpMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {ProtectTallyDumpCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: ProtectTallyDumpCommandOptions): string {
        return `protectDetails:${this.toStringProtectDetails(options.protectDetails)}`;
    }

    /**
     * Gets a textual representation of the ProtectTallyMessage
     *
     * @static
     * @param {ProtectDetails} data the ProtectDetails
     * @returns {string} the ProtectDetails textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringProtectDetails(data: ProtectDetails): string {
        return `( ${data} - ${ProtectDetails[data]} )`;
    }
}
