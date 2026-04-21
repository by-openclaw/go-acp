/**
 * ProtectInterrogateMessage command parameters
 *
 * @export
 * @interface ProtectInterrogateMessageCommandParams
 */
export interface ProtectInterrogateMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectInterrogateMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectInterrogateMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof ProtectInterrogateMessageCommandParams
     */

    destinationId: number;

    /**
     * Device ID - panel ID
     * + Range [0 - 1023]
     *
     * @type {number}
     * @memberof ProtectInterrogateMessageCommandParams
     */
    deviceId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectInterrogateMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectInterrogateMessageCommandParams} params the command parameters
     * @param {boolean} withDeviceId 'true' if the DeviceId must be part of the textual representation; otherwise 'false'
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectInterrogateMessageCommandParams, withDeviceId: boolean): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}${
            withDeviceId ? `,  Device:${params.deviceId}` : ''
        }`;
    }
}
