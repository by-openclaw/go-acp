/**
 * Source Association Names Response Message command params
 *
 * @export
 * @interface SourceAssociationNamesResponseCommandParams
 */
export interface SourceAssociationNamesResponseCommandParams {
    /**
     *
     * Matrix Number (0 – 19)
     * @type {number}
     * @memberof SourceAssociationNamesResponseCommandParams
     */
    matrixId: number;

    /**
     *
     * Level number, only applicable to Source Name type (0 -15)
     * @type {number}
     * @memberof SourceAssociationNamesResponseCommandParams
     */
    levelId: number;

    /**
     *
     * First Source number
     * @type {number}
     * @memberof SourceAssociationNamesResponseCommandParams
     */
    firstSourceId: number;

    /**
     *
     * Number of Sources Association Names to follow (in this message, maximum of 32 for 4 char names,
     * 16 for 8 char names and 10 for 12 char names).
     * @type {number}
     * @memberof SourceAssociationNamesResponseCommandParams
     */
    numberOfSourceAssociationNamesTofollow: number;

    /**
     * the labels size contains in the buffer depend of :
     * - 'lenOfNames' propertie :
     *  + if lenOfNames = 00 then max buffer is 04 char x 32 labels
     *  + if lenOfNames = 01 then max buffer is 08 char x 16 labels
     *  + if lenOfNames = 02 then max buffer is 12 char x 10 labels
     *
     * @type {string[]}
     * @memberof SourceNamesResponseCommandParams
     */
    sourceAssociationNameItems: string[];
}

/**
 * Utility class providing extra functionalities on the SourceNamesResponse command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    // TODO: add the list of nameCharsItems
    static toString(params: SourceAssociationNamesResponseCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, First Source number :${params.firstSourceId}, Number Of Source Association Names To Follow: ${params.numberOfSourceAssociationNamesTofollow},source Name Items :sourceNameItems[]`;
    }
}
