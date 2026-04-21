/**
 * AllDestinationsAssociationNamesRequestMessage command parameters
 *
 * @export
 * @interface AllDestinationsAssociationNamesRequestMessageCommandParams
 */
export interface AllDestinationsAssociationNamesRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof AllDestinationsAssociationNamesRequestMessageCommandParams
     */
    matrixId: number;
}

/**
 * Utility class providing extra functionalities on the AllDestinationsAssociationNamesRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {AllDestinationsAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {string} the ommand parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: AllDestinationsAssociationNamesRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}`;
    }
}
