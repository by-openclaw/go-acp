/**
 * AllSourceAssociationNamesRequestMessage command parameters
 *
 * @export
 * @interface AllSourceAssociationNamesRequestMessageCommandParams
 */
export interface AllSourceAssociationNamesRequestMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof AllSourceAssociationNamesRequestMessageCommandParams
     */
    matrixId: number;
}

/**
 * Utility class providing extra functionalities on the AllSourceAssociationNamesRequestMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {AllSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: AllSourceAssociationNamesRequestMessageCommandParams): string {
        return `Matrix:${params.matrixId}`;
    }
}
