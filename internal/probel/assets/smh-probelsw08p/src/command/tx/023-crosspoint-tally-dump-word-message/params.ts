/**
 * CrossPoint Tally Dump (Word) Message Command Input Paramameters
 * @export
 * @interface CrossPointTallyDumpWordCommandParams
 */
export interface CrossPointTallyDumpWordCommandParams {
    /**
     *
     * Matrix Number (0 – 15)
     * @type {number}
     * @memberof CrossPointTallyDumpWordCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number (0 -15)
     * @type {number}
     * @memberof CrossPointTallyDumpWordCommandParams
     */
    levelId: number;

    /**
     *
     * Number of tallies returned (Max 64)
     * @type {number}
     * @memberof CrossPointTallyDumpWordCommandParams
     */
    numberOfTalliesReturned: number;

    /**
     *
     * First Destination number
     * @type {number}
     * @memberof CrossPointTallyDumpWordCommandParams
     */
    firstDestinationId: number;

    /**
     *
     * SourceId Items number
     * @type {number[]}
     * @memberof CrossPointTallyDumpWordCommandParams
     */
    sourceIdItems: number[];
}

/**
 * Utility class providing extra functionalities on the CrossPointTallyDumpWordMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     *
     * @static
     * @param {CrossPointTallyDumpWordCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    // TODO : ADD sourceItems[]
    static toString(params: CrossPointTallyDumpWordCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.firstDestinationId}, Number Of Tallies Returned:${params.numberOfTalliesReturned}, sourceIdItems[]`;
    }
}
