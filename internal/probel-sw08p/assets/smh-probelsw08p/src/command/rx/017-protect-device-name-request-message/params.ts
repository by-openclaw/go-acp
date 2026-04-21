/**
 * ProtectDeviceNameRequestMessage command parameters
 *
 * @export
 * @interface ProtectDeviceNameRequestMessageCommandParams
 */
export interface ProtectDeviceNameRequestMessageCommandParams {
    /**
     * Device ID of the panel matrix
     * + Range [0 - 1023]
     *
     * @type {number}
     * @memberof ProtectDeviceNameRequestMessageCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectDeviceNameRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectDeviceNameRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectDeviceNameRequestMessageCommandParams): string {
        return `Device:${params.deviceId}`;
    }
}
