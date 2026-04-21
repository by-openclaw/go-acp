/**
 * CrossPointTallyDumpRequestMessage command parameters
 * @export
 * @interface CrossPointTallyDumpRequestMessageCommandParams
 */
export interface CrossPointTallyDumpRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointTallyDumpRequestMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointTallyDumpRequestMessageCommandParams
     */
    levelId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointTallyDumpRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointTallyDumpRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}`;
    }
}
