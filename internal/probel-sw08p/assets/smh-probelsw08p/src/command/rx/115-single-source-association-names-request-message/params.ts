/**
 * SingleDestinationAssociationNamesRequestMessage Command Input Paramameters
 * @export
 * @interface SingleSourceAssociationNamesRequestMessageCommandParams
 */
export interface SingleSourceAssociationNamesRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof SingleSourceAssociationNamesRequestMessageCommandParams
     */
    matrixId: number;

    /**
     * Sourcee number
     * + Rang[0-65535]
     *
     * @type {number}
     * @memberof SingleSourceAssociationNamesRequestMessageCommandParams
     */
    sourceId: number;
}

/**
 * Utility class providing extra functionalities on the SingleDestinationAssociationNamesRequestMessage command parameters
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
     * @param {SingleSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {string} command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: SingleSourceAssociationNamesRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Source:${params.sourceId}`;
    }
}
