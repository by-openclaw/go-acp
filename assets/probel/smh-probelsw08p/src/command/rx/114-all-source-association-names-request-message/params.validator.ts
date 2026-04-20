import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { AllSourceAssociationNamesRequestMessageCommandParams } from './params';
/**
 *  AllSourceAssociationNamesRequestMessageCommandParams command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<AllSourceAssociationNamesRequestMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<AllSourceAssociationNamesRequestMessageCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {AllSourceAssociationNamesRequestMessageCommandParams} data the command parameters
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: AllSourceAssociationNamesRequestMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {Record<string, LocaleData>} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        if (this.isMatrixIdOutOfRange()) {
            errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     *
     * @private
     * @returns {boolean} 'true' if the matrixId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }
}
