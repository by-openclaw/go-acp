import { CommandOptionsUtility, ProtectDetails } from './options';

/**
 * Protect Tally Dump Message command Items
 *
 * @export
 * @interface ProtectTallyDumpCommandItems
 */
export interface ProtectTallyDumpCommandItems {
    /**
     *
     * Device number
     * @type {number}
     * @memberof ProtectTallyDumpCommandItems
     */
    deviceId: number;
    /**
     * Protect Details
     *
     * @type {number}
     * @memberof ProtectTallyDumpCommandItems
     */
    protectedData: ProtectDetails;
}

/**
 * Utility class providing extra functionalities on the ProtectTallyDumpMessage command parameters
 *
 * @export
 * @class CommandItemsUtility
 */
export class CommandItemsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectTallyDumpCommandItems} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandItemsUtility
     */
    static toString(params: ProtectTallyDumpCommandItems): string {
        // TODO : Add deviceNumberProtectDataItems[]
        return `DeviceId:${params.deviceId}, Protect Details:${CommandOptionsUtility.toStringProtectDetails(
            params.protectedData
        )}`;
    }
}
