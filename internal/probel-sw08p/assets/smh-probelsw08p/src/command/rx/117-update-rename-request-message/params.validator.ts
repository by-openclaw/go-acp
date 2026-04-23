import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { UpdateRenameRequestCommandOptions } from './options';
import { UpdateRenameRequestCommandParams } from './params';

/**
 * UpdateRenameRequestCommandOptions command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<UpdateRenameRequestCommandParams>}
 */
export class CommandParamsValidator implements IValidator<UpdateRenameRequestCommandParams> {
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {UpdateRenameRequestCommandParams} data the command parameters
     * @param {UpdateRenameRequestCommandOptions} _options the command options (internally used)
     * @memberof CommandParamsValidator
     */
    constructor(
        readonly data: UpdateRenameRequestCommandParams,
        private readonly _options: UpdateRenameRequestCommandOptions
    ) {}

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
        if (this.isLevelIdOutOfRange()) {
            errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        }
        if (this.isFirstNameNumberIdOutOfRange()) {
            errors.firstNameNumber = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.FIRST_NAME_NUMBER_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceIdAndMaximumNumberOfNamesToFollowOutOfRange()) {
            errors.sourceIdAndMaximumNumberOfNames = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        if (this.isSourceNamesItemsOutOfRange()) {
            errors.sourceNamesItems = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    /**
     * Gets a boolean indicating whether the matrixId is out of range
     * + false = matrixId is included within the limits
     * + true = matrixId [0-255] is out of range
     *
     * @private
     * @returns {boolean} 'true' if matrixId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isMatrixIdOutOfRange(): boolean {
        return this.data.matrixId < 0 || this.data.matrixId > 255;
    }

    /**
     * Gets a boolean indicating whether the levelId is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     *
     * @private
     * @returns {boolean} 'true' if levelId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isLevelIdOutOfRange(): boolean {
        return this.data.levelId < 0 || this.data.levelId > 255;
    }

    /**
     * Gets a boolean indicating whether the firstNameNumber is out of range
     * + false = firstNameNumber is included within the limits
     * + true = firstNameNumber [0-65535] is out of range
     *
     * @private
     * @returns {boolean} 'true' if firstNameNumber is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isFirstNameNumberIdOutOfRange(): boolean {
        return this.data.firstNameNumber < 0 || this.data.firstNameNumber > 65535;
    }
    /**
     * Gets a boolean indicating whether the sum of first Name Number and length of nameCharsItems is out of range
     * + false = levelId is included within the limits
     * + true = levelId [0-255] is out of range
     *
     * @private
     * @returns {boolean}
     * @memberof CommandParamsValidator
     */
    private isSourceIdAndMaximumNumberOfNamesToFollowOutOfRange(): boolean {
        return this.data.firstNameNumber < 0 || this.data.firstNameNumber + this.data.nameCharsItems.length > 65535;
    }

    /**
     * Gets a boolean indicating whether the nameCharsItem is out of range
     * + false = nameCharsItem is included within the limits of the byteMaximumNumberOfNames
     * + true = nameCharsItem  [depend of the byteMaximumNumberOfNames] is out of range
     *
     * @private
     * @returns {boolean} 'true' if nameCharsItem is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isSourceNamesItemsOutOfRange(): boolean {
        if (this.data.nameCharsItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.nameCharsItems.length; byteIndex++) {
            const data = this.data.nameCharsItems[byteIndex];
            if (data.length < 1 || data.length > this._options.lengthOfNames.byteLength) {
                return true;
            }
        }
        return false;
    }
}
