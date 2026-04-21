import { SingleDestinationAssociationNamesRequestMessageCommand } from '../../../../src/command/rx/103-single-destination-association-names-request-message/command';
import { SingleDestinationAssociationNamesRequestMessageCommandParams } from '../../../../src/command/rx/103-single-destination-association-names-request-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { SingleDestinationAssociationNamesRequestMessageCommandOptions } from '../../../../src/command/rx/103-single-destination-association-names-request-message/options';
import { LengthOfNamesRequired } from '../../../../src/command/shared/length-of-names-required';

describe('Single Destination Association Names Request Message command)', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                matrixId: 0,
                destinationId: 0
            };
            const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                matrixId: -1,
                destinationId: -1
            };

            const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: -1
            };
            // Act
            const fct = () => new SingleDestinationAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                matrixId: 256,
                destinationId: 65536
            };

            const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: 10
            };
            // Act
            const fct = () => new SingleDestinationAssociationNamesRequestMessageCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Single Destination Association Names Request Message CMD_103_0X67', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.RX.GENERAL.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 0
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 00 00 00', // data
                    5, // bytesCount
                    0x94, // checksum
                    '10 02 67 00 00 00 00 05 94 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 0
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 01 00 00', // data
                    5, // bytesCount
                    0x93, // checksum
                    '10 02 67 00 01 00 00 05 93 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 0
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 02 00 00', // data
                    5, // bytesCount
                    0x92, // checksum
                    '10 02 67 00 02 00 00 05 92 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 896
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 01 03 80', // data
                    5, // bytesCount
                    0x10, // checksum
                    '10 02 67 00 01 03 80 05 10 10 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 65534
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 00 FF FE', // data
                    5, // bytesCount
                    0x97, // checksum
                    '10 02 67 00 00 FF FE 05 97 10 03' // buffer
                );
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 0,
                    destinationId: 65535
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '67 00 00 FF FF', // data
                    5, // bytesCount
                    0x96, // checksum
                    '10 02 67 00 00 FF FF 05 96 10 03' // buffer
                );
            });
        });

        describe('Extended General General Single Destination Association Names Request Message CMD_231_0Xe7', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.RX.EXTENDED.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });
            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    destinationId: 896
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E7 10 00 03 80', // data
                    5, // bytesCount
                    0x81, // checksum
                    '10 02 E7 10 10 00 03 80 05 81 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    destinationId: 896
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.EIGHT_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E7 10 01 03 80', // data
                    5, // bytesCount
                    0x80, // checksum
                    '10 02 E7 10 10 01 03 80 05 80 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    destinationId: 896
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E7 10 02 03 80', // data
                    5, // bytesCount
                    0x7f, // checksum
                    '10 02 E7 10 10 02 03 80 05 7F 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.RX.EXTENDED.SINGLE_DESTINATIONS_ASSOCIATION_NAMES_REQUEST_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                    matrixId: 16,
                    destinationId: 16
                };

                const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                    lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
                };
                // Act
                const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'E7 10 02 00 10', // data
                    5, // bytesCount
                    0xf2, // checksum
                    '10 02 E7 10 10 02 00 10 10 05 F2 10 03' // buffer
                );
            });
        });
    });

    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                matrixId: 0,
                destinationId: 0
            };

            const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });

        it('Should log Extended general command description', () => {
            // Arrange
            const params: SingleDestinationAssociationNamesRequestMessageCommandParams = {
                matrixId: 16,
                destinationId: 896
            };

            const options: SingleDestinationAssociationNamesRequestMessageCommandOptions = {
                lengthOfNames: LengthOfNamesRequired.TWELVE_CHAR_NAMES
            };
            // Act
            const command = new SingleDestinationAssociationNamesRequestMessageCommand(params, options);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
