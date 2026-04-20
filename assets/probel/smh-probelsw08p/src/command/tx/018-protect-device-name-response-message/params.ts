/**
 * Protect Device Name Response Message
 * @export
 * @interface ProtectDeviceNameResponseCommandParams
 */
export interface ProtectDeviceNameResponseCommandParams {
    /**
     *
     * Device number
     * Device (0- 1023 Devices)
     * @type {number}
     * @memberof ProtectDeviceNameRequestCommandParams
     */
    deviceId: number;

    /**
     *
     * Fixed Eight character ASCII device name
     * @type {string}
     * @memberof ProtectDeviceNameResponseCommandParams
     */
    deviceName: string;
}

/**
 * Utility class providing extra functionalities on the MasterProtectConnectMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {MasterProtectConnectMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectDeviceNameResponseCommandParams): string {
        return `Device:${params.deviceId}, Device:${params.deviceName}`;
    }
}
