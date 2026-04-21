/**
 * CrossPointTallyMessage Command Input Paramameters
 *
 * @export
 * @interface CrossPointTallyMessageCommandParams
 */
export interface CrossPointTallyMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointTallyMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointTallyMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointTallyMessageCommandParams
     */
    destinationId: number;

    /**
     * Source ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointTallyMessageCommandParams
     */

    sourceId: number;

    /**
     * Stastus  (For future use)
     * + forced to 0
     * @type {number}
     * @memberof CrossPointTallyMessageCommandParams
     */
    statusId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointTallyMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointInterrogateMessageCommandParams} the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointTallyMessageCommandParams, withStatusId: boolean): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source:${
            params.sourceId
        }${withStatusId ? `, Status:${params.statusId}` : ''}`;
    }
}
