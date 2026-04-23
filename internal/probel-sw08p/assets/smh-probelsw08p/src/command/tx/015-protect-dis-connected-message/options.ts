/**
 * Returns Probel SW-P-08 - Protect Dis-Connected Message CMD_015_0X0f
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
 * Returns the ProtectDis-ConnectMessage command options
 *
 * @export
 * @interface ProtectDiscConnectedCommandOptions
 */
export interface ProtectDiscConnectedCommandOptions {
    /**
     * Protect Details
     *
     * @type {ProtectDetails}
     * @memberof ProtectDiscConnectedCommandOptions
     */
    protectDetails: ProtectDetails;
}

/**
 * Utility class providing some extra functionality on the ProtectDis-ConnectMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {ProtectDiscConnectedCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: ProtectDiscConnectedCommandOptions): string {
        return `protectDetails:${this.toStringProtectDetails(options.protectDetails)}`;
    }

    /**
     * Gets a textual representation of the ProtectDis-ConnectMessage
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
