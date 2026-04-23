/**
 * UpdateRenameRequest command parameters
 *
 * @export
 * @interface UpdateRenameRequestCommandParams
 */
export interface UpdateRenameRequestCommandParams {
    /**
     *
     * Matrix Number (0 – 19)
     *
     * @type {number}
     * @memberof UpdateRenameRequestCommandParams
     */
    matrixId: number;

    /**
     * Level number, only applicable to Source Name type (0 -15)
     * @type {number}
     * @memberof UpdateRenameRequestCommandParams
     */
    levelId: number;

    /**
     * First Name number, only applicable to Source Name type (0 -65535)
     *
     * @type {number}
     * @memberof UpdateRenameRequestCommandParams
     */
    firstNameNumber: number;

    /**
     * Number of Source Names to follow (in this message, maximum of 32 for 4 char names,
     * 16 for 8 char names, 10 for 12 char names, 8 for 16 char names).
     *
     * @type {number}
     * @memberof UpdateRenameRequestCommandParams
     */
    numberOfNamesToFollow: number;

    /**
     * This string[] must contains a list of labels depending of the size of characters selected,
     * the first element (source or target number)
     *
     * the labels size contains in the buffer depend of :
     * - 'LengthOfNamesRequired' propertie :
     *  + if LengthOfNamesRequired = 00 then max buffer is 04 char x 32 labels
     *  + if LengthOfNamesRequired = 01 then max buffer is 08 char x 16 labels
     *  + if LengthOfNamesRequired = 02 then max buffer is 12 char x 10 labels
     *  + if LengthOfNamesRequired = 03 then max buffer is 16 char x 08 labels
     *
     * @type {string[]}
     * @memberof UpdateRenameRequestCommandParams
     */
    nameCharsItems: string[];
}

/**
 * Utility class providing extra functionalities on the UpdateRenameRequest command parameters
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
    static toString(params: UpdateRenameRequestCommandParams, whitSourceName: boolean): string {
        return (
            `Matrix:${params.matrixId}${whitSourceName ? `, Level:${params.levelId}` : ''}` +
            `, First name number :${params.firstNameNumber}, Name Chars :nameCharsItems[]`
        );
    }
}
