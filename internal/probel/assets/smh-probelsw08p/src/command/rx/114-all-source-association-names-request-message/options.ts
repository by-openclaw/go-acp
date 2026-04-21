import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * AllSourceAssociationNamesRequestMessage command options
 *
 * @export
 * @interface AllSourceAssociationNamesRequestMessageCommandOptions
 */
export interface AllSourceAssociationNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required
     *
     * @type {LengthOfNamesRequired}
     * @memberof AllSourceAssociationNamesRequestMessageCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing extra functionalities on the AllSourceAssociationNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {AllSourceAssociationNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: AllSourceAssociationNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the LengthOfNamesRequired
     *
     * @param {LengthOfNamesRequired} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
