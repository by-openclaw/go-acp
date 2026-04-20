/**
 * ProtectTallyDumpRequestMessage command parameters
 *
 * @export
 * @interface ProtectTallyDumpRequestMessageCommandParams
 */
export interface ProtectTallyDumpRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectTallyDumpRequestMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof ProtectTallyDumpRequestMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof ProtectTallyDumpRequestMessageCommandParams
     */
    destinationId: number;
}

/**
 * Utility class providing extra functionalities on the ProtectTallyDumpRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {ProtectTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: ProtectTallyDumpRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}`;
    }
}
